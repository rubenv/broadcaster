package broadcaster

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"

	"code.google.com/p/go-uuid/uuid"
)

type longpollConnection struct {
	Token  string
	Server *Server
}

func newLongpollConnection(w http.ResponseWriter, r *http.Request, m clientMessage, s *Server) (*longpollConnection, error) {
	conn := &longpollConnection{
		Server: s,
		Token:  uuid.New(),
	}

	err := conn.handshake(w, r, m)
	if err != nil {
		return nil, err
	}

	return conn, nil
}

func (c *longpollConnection) handshake(w http.ResponseWriter, r *http.Request, auth clientMessage) error {
	// Expect auth packet first.
	if auth.Type() != AuthMessage {
		w.WriteHeader(401)
		c.Reply(w, clientMessage{"type": AuthFailedMessage, "reason": "Auth expected"})
		return errors.New("Auth expected")
	}

	if c.Server.CanConnect != nil && !c.Server.CanConnect(auth) {
		w.WriteHeader(401)
		c.Reply(w, clientMessage{"type": AuthFailedMessage, "reason": "Unauthorized"})
		return errors.New("Unauthorized")
	}

	json.NewEncoder(w).Encode([]clientMessage{
		clientMessage{"type": AuthOKMessage},
	})

	hub := c.Server.hub
	hub.NewClient <- c

	return nil
}

func (c *longpollConnection) Reply(w http.ResponseWriter, m ...clientMessage) {
	json.NewEncoder(w).Encode(m)
}

func (c *longpollConnection) Send(channel, message string) {
}

func (c *longpollConnection) Handle(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode([]clientMessage{
		clientMessage{
			"type": "fail",
		},
	})
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
	data["type"] = AuthMessage

	err := t.Send(data)
	if err != nil {
		return err
	}

	go t.poll()
	return nil
}

func (t *longpollClientTransport) Close() error {
	close(t.messages)
	return nil
}

func (t *longpollClientTransport) Send(data clientMessage) error {
	data["token"] = t.token

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

func (t *longpollClientTransport) poll() {
	// TODO: Keep polling for messages.
}
