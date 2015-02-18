package broadcaster

import (
	"encoding/binary"
	"net/http"

	"github.com/gorilla/websocket"
)

type websocketConnection struct {
	Conn   *websocket.Conn
	Server *Server
}

func newWebsocketConnection(w http.ResponseWriter, r *http.Request, s *Server) {
	conn := &websocketConnection{
		Server: s,
	}
	conn.handshake(w, r)
}

func (c *websocketConnection) handshake(w http.ResponseWriter, r *http.Request) {
	conn, err := c.Server.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	c.Conn = conn

	// Expect auth packet first.
	auth := clientMessage{}
	err = conn.ReadJSON(&auth)
	if err != nil || auth.Type() != AuthMessage {
		conn.WriteJSON(clientMessage{"__type": AuthFailedMessage, "reason": "Auth expected"})
		c.Close(401, "Auth expected")
		return
	}

	if c.Server.CanConnect != nil && !c.Server.CanConnect(auth) {
		conn.WriteJSON(clientMessage{"__type": AuthFailedMessage, "reason": "Unauthorized"})
		c.Close(401, "Unauthorized")
		return
	}

	conn.WriteJSON(clientMessage{"__type": AuthOKMessage})

	hub := c.Server.hub
	hub.NewClient <- c

	defer func() {
		hub.ClientDisconnect <- c
		conn.Close()
	}()
	m := clientMessage{}
	for {
		err := conn.ReadJSON(&m)
		if err != nil {
			c.Close(400, err.Error())
			break
		}

		switch m.Type() {
		case SubscribeMessage:
			channel := m["channel"]
			if c.Server.CanSubscribe != nil && !c.Server.CanSubscribe(auth, channel) {
				c.Close(403, "Channel refused")
				continue
			}

			s := &subscription{
				Client:  c,
				Channel: channel,
				Done:    make(chan error, 0),
			}

			hub.Subscribe <- s

			err := <-s.Done
			if err != nil {
				conn.WriteJSON(clientMessage{
					"__type":  SubscribeErrorMessage,
					"channel": channel,
					"error":   err.Error(),
				})
			} else {
				conn.WriteJSON(clientMessage{
					"__type":  SubscribeOKMessage,
					"channel": channel,
				})
			}

		case UnsubscribeMessage:
			channel := m["channel"]

			s := &subscription{
				Client:  c,
				Channel: channel,
				Done:    make(chan error, 0),
			}

			hub.Unsubscribe <- s
			conn.WriteJSON(clientMessage{
				"__type":  UnsubscribeOKMessage,
				"channel": channel,
			})

		default:
			c.Close(400, "Unexpected message")
			break
		}
	}
}

func (c *websocketConnection) Close(code uint16, msg string) {
	payload := make([]byte, 2)
	binary.BigEndian.PutUint16(payload, code)
	payload = append(payload, []byte(msg)...)
	c.Conn.WriteMessage(websocket.CloseMessage, payload)
	c.Conn.Close()
}

func (c *websocketConnection) Send(channel, message string) {
	c.Conn.WriteJSON(clientMessage{
		"__type":  MessageMessage,
		"channel": channel,
		"body":    message,
	})
}

// Client transport
type websocketClientTransport struct {
	conn   *websocket.Conn
	client *Client
}

func (t *websocketClientTransport) Connect(authData map[string]string) error {
	conn, _, err := websocket.DefaultDialer.Dial(t.client.url(), nil)
	if err != nil {
		return err
	}

	t.conn = conn

	// Authenticate
	if !t.client.skip_auth {
		data := authData
		if data == nil {
			data = make(map[string]string)
		}
		data["__type"] = AuthMessage
		err := t.Send(data)
		if err != nil {
			return err
		}
	}
	return nil
}

func (t *websocketClientTransport) Close() error {
	return t.conn.Close()
}

func (t *websocketClientTransport) Send(data clientMessage) error {
	return t.conn.WriteJSON(data)
}

func (t *websocketClientTransport) Receive() (clientMessage, error) {
	m := clientMessage{}
	err := t.conn.ReadJSON(&m)
	return m, err
}

func (t *websocketClientTransport) onConnect() {
}
