package broadcaster

import (
	"bytes"
	"encoding/json"
	"math/rand"
	"net/http"
	"time"

	"code.google.com/p/go-uuid/uuid"
)

type longpollConnection struct {
	Token    string
	Server   *Server
	AuthData clientMessage

	combining bool
	messages  chan clientMessage
	deadline  <-chan time.Time

	subscribe   chan string
	unsubscribe chan string
}

func handleLongpollConnection(w http.ResponseWriter, r *http.Request, s *Server) error {
	m := clientMessage{}
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
		messages, err := conn.poll()
		if err != nil {
			return err
		}
		longpollReply(w, messages...)
	} else {
		switch m.Type() {
		case SubscribeMessage:
			auth, err := redis.GetSession(m.Token())
			if err != nil {
				return err
			}

			channel := m["channel"]
			if s.CanSubscribe != nil && !s.CanSubscribe(auth, channel) {
				longpollReply(w, clientMessage{
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

		default:
			longpollReply(w, newMessage(UnknownMessage))
			/*
				case UnsubscribeMessage:
					channel := m["channel"]

					s := &subscription{
						Client:  c,
						Channel: channel,
						Done:    make(chan error, 0),
					}

					hub.Unsubscribe <- s
					c.Reply(w, clientMessage{
						"__type":  UnsubscribeOKMessage,
						"channel": channel,
					})

			*/
		}
	}

	return nil
}

func (c *longpollConnection) handshake(w http.ResponseWriter, r *http.Request, auth clientMessage) error {
	// Expect auth packet first.
	if auth.Type() != AuthMessage {
		w.WriteHeader(401)
		longpollReply(w, clientMessage{"__type": AuthFailedMessage, "reason": "Auth expected"})
		return nil
	}

	if c.Server.CanConnect != nil && !c.Server.CanConnect(auth) {
		w.WriteHeader(401)
		longpollReply(w, clientMessage{"__type": AuthFailedMessage, "reason": "Unauthorized"})
		return nil
	}

	// Store session
	err := c.Server.redis.StoreSession(c.Token, auth)
	if err != nil {
		return err
	}

	longpollReply(w, clientMessage{"__type": AuthOKMessage, "__token": c.Token})

	return nil
}

func (c *longpollConnection) poll() ([]clientMessage, error) {
	redis := c.Server.redis
	err := redis.LongpollPing(c.Token)
	if err != nil {
		return nil, err
	}

	c.deadline = time.After(c.Server.Timeout - c.Server.PollTime)
	c.messages = make(chan clientMessage, 10)
	c.subscribe = make(chan string, 1)
	c.unsubscribe = make(chan string, 1)

	hub := c.Server.hub

	err = hub.Connect(c)
	if err != nil {
		return nil, err
	}
	defer hub.Disconnect(c)

	channels, err := redis.LongpollGetChannels(c.Token)
	if err != nil {
		return nil, err
	}
	for _, channel := range channels {
		err := hub.Subscribe(c, channel)
		if err != nil {
			return nil, err
		}
		defer hub.Unsubscribe(c, channel)
	}

	messages := []clientMessage{}

loop:
	for {
		select {
		case <-c.deadline:
			break loop
		case channel := <-c.subscribe:
			hub.Subscribe(c, channel)
		case channel := <-c.unsubscribe:
			hub.Unsubscribe(c, channel)
		case m := <-c.messages:
			messages = append(messages, m)
		}
	}

	return messages, nil
}

func longpollReply(w http.ResponseWriter, m ...clientMessage) {
	json.NewEncoder(w).Encode(m)
}

func (c *longpollConnection) Send(channel, message string) {
	if !c.combining {
		c.deadline = time.After(c.Server.PollTime)
		c.combining = true
	}
	c.messages <- newBroadcastMessage(channel, message)
}

func (c *longpollConnection) Process(t string, args []string) {
	switch t {
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
	messages   chan clientMessage
	err        error
	token      string
	httpClient http.Client
}

func newlongpollClientTransport(c *Client) *longpollClientTransport {
	return &longpollClientTransport{
		client:     c,
		messages:   make(chan clientMessage, 10),
		httpClient: http.Client{},
	}
}

func (t *longpollClientTransport) Connect(authData map[string]string) error {
	data := authData
	if data == nil {
		data = make(map[string]string)
	}
	data["__type"] = AuthMessage

	if t.client.skip_auth {
		data = clientMessage{}
	}

	return t.Send(data)
}

func (t *longpollClientTransport) Close() error {
	t.running = false
	close(t.messages)
	return nil
}

func (t *longpollClientTransport) Send(data clientMessage) error {
	data["__token"] = t.token

	buf, err := json.Marshal(data)
	if err != nil {
		return err
	}

	url := t.client.url()
	resp, err := t.httpClient.Post(url, "application/json", bytes.NewBuffer(buf))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	result := []clientMessage{}
	json.NewDecoder(resp.Body).Decode(&result)
	for _, v := range result {
		t.messages <- v
	}
	return nil
}

func (t *longpollClientTransport) Receive() (clientMessage, error) {
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
	data := clientMessage{
		"__type":  PollMessage,
		"__token": t.token,
	}

	buf, _ := json.Marshal(data)

	for t.running {

		url := t.client.url()
		resp, err := t.httpClient.Post(url, "application/json", bytes.NewBuffer(buf))
		if err != nil {
			// Random backoff
			<-time.After(time.Duration(rand.Int63n(int64(t.client.Timeout / 2))))
			continue
		}
		defer resp.Body.Close()
	}
}
