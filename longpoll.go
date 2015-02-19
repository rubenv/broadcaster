package broadcaster

import (
	"bytes"
	"encoding/json"
	"net/http"

	"code.google.com/p/go-uuid/uuid"
)

type longpollConnection struct {
	Token    string
	Server   *Server
	AuthData clientMessage
}

func handleLongpollConnection(w http.ResponseWriter, r *http.Request, s *Server) error {
	m := clientMessage{}
	json.NewDecoder(r.Body).Decode(&m)

	//hub := s.hub
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
	} else if m.Type() == PollMessage {
		// TODO
	} else {

		/*conn := &longpollConnection{
			Server:   s,
			Token:    uuid.New(),
			AuthData: m,
		}*/

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

			/*s := &longpollSubscription{
				Token:   m.Token(),
				Channel: channel,
			}

			hub.LongpollSubscribe <- s*/
			longpollReply(w)

			/*
				s := &subscription{
					Client:  c,
					Channel: channel,
					Done:    make(chan error, 0),
				}

				hub.Subscribe <- s

				err := <-s.Done
				if err != nil {
					c.Reply(w, clientMessage{
						"__type":  SubscribeErrorMessage,
						"channel": channel,
						"reason":  err.Error(),
					})
				} else {
					c.Reply(w, clientMessage{
						"__type":  SubscribeOKMessage,
						"channel": channel,
					})
				}
			*/

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

				default:
					c.Reply(w, clientMessage{
						"__type": UnknownMessage,
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

func longpollReply(w http.ResponseWriter, m ...clientMessage) {
	json.NewEncoder(w).Encode(m)
}

func (c *longpollConnection) Send(channel, message string) {
}

func (c *longpollConnection) Handle(w http.ResponseWriter, r *http.Request, m clientMessage) {
	//hub := c.Server.hub

}

// Client transport
type longpollClientTransport struct {
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
	go t.poll()
}

func (t *longpollClientTransport) poll() {
	// TODO: Keep polling for messages.
}
