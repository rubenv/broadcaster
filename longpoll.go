package broadcaster

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/pborman/uuid"
	"go.uber.org/atomic"
)

type longpollConnection struct {
	Token    string
	Server   *Server
	AuthData ClientMessage

	combining bool
	messages  chan ClientMessage
	deadline  <-chan time.Time

	subscribe   chan string
	unsubscribe chan string
	transfer    chan string
}

func handleLongpollConnection(w http.ResponseWriter, r *http.Request, s *Server) error {
	m := ClientMessage{}
	json.NewDecoder(r.Body).Decode(&m)

	redis := s.redis

	token := m.Token()
	connected := false
	if m.Token() != "" {
		c, err := redis.IsConnected(token)
		if err != nil {
			return err
		}
		connected = c
	}

	if !connected {
		conn := &longpollConnection{
			Server:   s,
			Token:    uuid.New(),
			AuthData: m,
		}
		return conn.handshake(w, r, m)
	}

	// Existing connection
	conn := &longpollConnection{
		Server: s,
		Token:  m.Token(),
	}

	if m.Type() == PollMessage {
		return conn.poll(w, m["seq"].(string))
	} else {
		switch m.Type() {
		case SubscribeMessage:
			auth, err := redis.GetSession(m.Token())
			if err != nil {
				return err
			}

			channel := m.Channel()
			if s.CanSubscribe != nil && !s.CanSubscribe(auth, channel) {
				longpollReply(w, ClientMessage{
					"__type":  SubscribeErrorMessage,
					"channel": channel,
					"reason":  "Channel refused",
				})
				return nil
			}

			err = redis.LongpollSubscribe(m.Token(), channel)
			if err != nil {
				longpollReply(w, newChannelErrorMessage(SubscribeErrorMessage, channel, err))
				return nil
			}

			longpollReply(w, newChannelMessage(SubscribeOKMessage, channel))

		case UnsubscribeMessage:
			channel := m.Channel()
			err := redis.LongpollUnsubscribe(m.Token(), channel)
			if err != nil {
				longpollReply(w, newChannelErrorMessage(UnsubscribeErrorMessage, channel, err))
				return nil
			}

			longpollReply(w, newChannelMessage(UnsubscribeOKMessage, channel))

		default:
			longpollReply(w, newMessage(UnknownMessage))
		}
	}

	return nil
}

func (c *longpollConnection) handshake(w http.ResponseWriter, r *http.Request, auth ClientMessage) error {
	// Expect auth packet first.
	if auth.Type() != AuthMessage {
		w.WriteHeader(401)
		longpollReply(w, ClientMessage{"__type": AuthFailedMessage, "reason": "Auth expected"})
		return nil
	}

	if c.Server.CanConnect != nil && !c.Server.CanConnect(auth) {
		w.WriteHeader(401)
		longpollReply(w, ClientMessage{"__type": AuthFailedMessage, "reason": "Unauthorized"})
		return nil
	}

	// Store session
	err := c.Server.redis.StoreSession(c.Token, auth)
	if err != nil {
		return err
	}

	longpollReply(w, ClientMessage{"__type": AuthOKMessage, "__token": c.Token})

	return nil
}

func (c *longpollConnection) poll(w http.ResponseWriter, seq string) error {
	redis := c.Server.redis
	err := redis.LongpollPing(c.Token)
	if err != nil {
		return err
	}

	c.deadline = time.After(c.Server.Timeout - c.Server.PollTime)
	c.messages = make(chan ClientMessage, 10)
	c.subscribe = make(chan string, 10)
	c.unsubscribe = make(chan string, 10)
	c.transfer = make(chan string, 10)

	hub := c.Server.hub

	err = hub.Connect(c)
	if err != nil {
		return err
	}

	// Resubscribe to all the channels that are tracked by this connection.
	channels, err := redis.LongpollGetChannels(c.Token)
	if err != nil {
		return err
	}
	for _, channel := range channels {
		err := hub.Subscribe(c, channel)
		if err != nil {
			hub.Disconnect(c)
			return err
		}
	}

	// Kill other listeners
	go redis.LongpollTransfer(c.Token, seq)

	// Ensure we broadcast the backlog
	go redis.LongpollGetBacklog(c.Token, c.messages)

	// Wait until we either time-out or until the message deadline hits.
	// The initial deadline is configured to the polling Timeout length.
	// Once the first message comes in, this is shortened to PollTime.
	//
	// Also handles notifications of (un)subscription which may have happend
	// while waiting.
	messages := []ClientMessage{}
	transferred := c.listen(seq, func(m ClientMessage) {
		if !c.combining {
			c.deadline = time.After(c.Server.PollTime)
			c.combining = true
		}
		messages = append(messages, m)
	})
	longpollReply(w, messages...)

	if transferred {
		hub.Disconnect(c)
		return nil
	}

	go func() {
		// Listens for new messages until a new client connects. This ensures we
		// don't lose any messages
		c.deadline = time.After(c.Server.Timeout)
		c.listen(seq, func(m ClientMessage) {
			redis.LongpollBacklog(c.Token, m)
		})
		hub.Disconnect(c)
	}()

	return nil
}

func (c *longpollConnection) listen(seq string, onMessage func(m ClientMessage)) bool {
	hub := c.Server.hub

	for {
		select {
		case <-c.deadline:
			return false
		case channel := <-c.subscribe:
			hub.Subscribe(c, channel)
		case channel := <-c.unsubscribe:
			hub.Unsubscribe(c, channel)
		case s := <-c.transfer:
			if s != seq {
				return true
			}
		case m := <-c.messages:
			onMessage(m)
		}
	}
}

func longpollReply(w http.ResponseWriter, m ...ClientMessage) {
	json.NewEncoder(w).Encode(m)
}

func (c *longpollConnection) Send(channel, message string) {
	c.messages <- newBroadcastMessage(channel, message)
}

func (c *longpollConnection) Process(t string, args []string) {
	switch t {
	case "transfer":
		select {
		case c.transfer <- args[0]:
		default: // Receiver might be dead and buffer is full, discard
		}
	case "subscribe":
		c.subscribe <- args[0]
	case "unsubscribe":
		c.unsubscribe <- args[0]
	}
}

func (c *longpollConnection) GetToken() string {
	return c.Token
}

// Client transport
type longpollClientTransport struct {
	poll_lock    sync.Mutex
	running      *atomic.Bool
	client       *Client
	messages     chan ClientMessage
	token        string
	httpClient   http.Client
	httpReq      *http.Request
	httpReq_lock sync.Mutex
	call         int

	err      error
	err_lock sync.Mutex
}

func newlongpollClientTransport(c *Client) clientTransport {
	return &longpollClientTransport{
		running:  atomic.NewBool(false),
		client:   c,
		messages: make(chan ClientMessage, 10),
		httpClient: http.Client{
			Transport: http.DefaultTransport,
		},
	}
}

func (t *longpollClientTransport) Connect(authData ClientMessage) error {
	data := authData
	if data == nil {
		data = make(ClientMessage)
	}
	data["__type"] = AuthMessage

	if t.client.skip_auth {
		data = ClientMessage{}
	}

	return t.Send(data)
}

func (t *longpollClientTransport) Close() error {
	t.running.Store(false)

	t.httpReq_lock.Lock()
	defer t.httpReq_lock.Unlock()
	if t.httpReq != nil {
		if transport, ok := t.httpClient.Transport.(*http.Transport); ok {
			transport.CancelRequest(t.httpReq)
		}
		t.httpReq = nil
	}
	t.err_lock.Lock()
	t.err = io.EOF
	t.err_lock.Unlock()
	return nil
}

func (t *longpollClientTransport) Send(data ClientMessage) error {
	data["__token"] = t.token

	buf, err := json.Marshal(data)
	if err != nil {
		return err
	}

	url := t.client.url(ClientModeLongPoll)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(buf))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	if t.client.UserAgent != "" {
		req.Header.Set("User-Agent", t.client.UserAgent)
	}

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusUnauthorized {
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("Non OK status code: %d, failed to read body: %s", resp.StatusCode, err)
		}

		return fmt.Errorf("Non OK status code: %d -> %s", resp.StatusCode, string(body))
	}

	result := []ClientMessage{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		return err
	}
	for _, v := range result {
		t.messages <- v
	}
	return nil
}

func (t *longpollClientTransport) Receive() (ClientMessage, error) {
	m, ok := <-t.messages
	if !ok {
		return nil, t.getErr()
	}
	if m.Type() == AuthFailedMessage {
		return nil, &CloseError{
			Code: 4401,
			Text: m.Reason(),
		}
	}
	if m.Type() == AuthOKMessage {
		t.token = m.Token()
	}
	return m, nil
}

func (t *longpollClientTransport) onConnect() {
	t.running.Store(true)
	go t.poll()
}

func (t *longpollClientTransport) poll() {
	t.poll_lock.Lock()
	defer t.poll_lock.Unlock()

	for t.running.Load() {
		err := t.pollOnce()
		if err != nil {
			return
		}
	}

	t.httpReq_lock.Lock()
	defer t.httpReq_lock.Unlock()
	t.httpReq = nil
	close(t.messages)
}

func (t *longpollClientTransport) pollOnce() error {
	url := t.client.url(ClientModeLongPoll)
	data := ClientMessage{
		"__type":  PollMessage,
		"__token": t.token,
		"seq":     strconv.Itoa(t.call),
	}
	t.call++

	buf, err := json.Marshal(data)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(buf))
	if err != nil {
		t.client.disconnected()
		return err
	}

	t.httpReq_lock.Lock()
	t.httpReq = req
	t.httpReq_lock.Unlock()
	req.Header.Set("Content-Type", "application/json")
	if t.client.UserAgent != "" {
		req.Header.Set("User-Agent", t.client.UserAgent)
	}

	resp, err := t.httpClient.Do(req)
	if err != nil {
		t.client.disconnected()
		return err
	}

	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusUnauthorized {
		t.client.disconnected()
		return err
	}

	if !t.running.Load() {
		return nil
	}

	result := []ClientMessage{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		return err
	}
	for _, v := range result {
		t.messages <- v
	}

	return nil
}

func (t *longpollClientTransport) getErr() error {
	t.err_lock.Lock()
	defer t.err_lock.Unlock()
	return t.err
}
