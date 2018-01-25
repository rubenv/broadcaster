package broadcaster

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strconv"
	"time"

	"github.com/pborman/uuid"
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
	c.subscribe = make(chan string, 1)
	c.unsubscribe = make(chan string, 1)
	c.transfer = make(chan string, 1)

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
		c.transfer <- args[0]
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
	running    bool
	client     *Client
	messages   chan ClientMessage
	err        error
	token      string
	httpClient http.Client
	httpReq    *http.Request
	call       int
}

func newlongpollClientTransport(c *Client) *longpollClientTransport {
	return &longpollClientTransport{
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
	t.running = false
	if t.httpReq != nil {
		if transport, ok := t.httpClient.Transport.(*http.Transport); ok {
			transport.CancelRequest(t.httpReq)
		}
	}
	t.err = io.EOF
	return nil
}

func (t *longpollClientTransport) Send(data ClientMessage) error {
	data["__token"] = t.token

	buf, err := json.Marshal(data)
	if err != nil {
		return err
	}

	url := t.client.url(ClientModeLongPoll)
	resp, err := t.httpClient.Post(url, "application/json", bytes.NewBuffer(buf))
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
		return nil, t.err
	}
	if m.Type() == AuthOKMessage {
		t.token = m.Token()
	}
	return m, nil
}

func (t *longpollClientTransport) onConnect() {
	t.running = true
	go t.poll()
}

func (t *longpollClientTransport) poll() {
	data := ClientMessage{
		"__type":  PollMessage,
		"__token": t.token,
		"seq":     strconv.Itoa(t.call),
	}
	t.call++

	buf, err := json.Marshal(data)
	if err != nil {
		return
	}

	for t.running {
		t.pollOnce(buf)
	}

	t.httpReq = nil
	close(t.messages)
}

func (t *longpollClientTransport) pollOnce(buf []byte) {
	url := t.client.url(ClientModeLongPoll)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(buf))
	if err != nil {
		t.client.disconnected()
		return
	}

	t.httpReq = req
	t.httpReq.Header.Set("Content-Type", "application/json")
	resp, err := t.httpClient.Do(t.httpReq)
	if err != nil {
		t.client.disconnected()
		return
	}

	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusUnauthorized {
		t.client.disconnected()
		return
	}

	if !t.running {
		return
	}

	result := []ClientMessage{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		return
	}
	for _, v := range result {
		t.messages <- v
	}
}
