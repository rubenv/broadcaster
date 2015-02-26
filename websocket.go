package broadcaster

import (
	"encoding/binary"
	"errors"
	"net/http"

	"code.google.com/p/go-uuid/uuid"

	"github.com/gorilla/websocket"
)

type websocketConnection struct {
	Token    string
	Conn     *websocket.Conn
	Server   *Server
	AuthData clientMessage
}

func newWebsocketConnection(w http.ResponseWriter, r *http.Request, s *Server) {
	conn := &websocketConnection{
		Server: s,
		Token:  uuid.New(),
	}
	err := conn.handshake(w, r)
	if err != nil {
		if conn.Conn != nil {
			conn.Conn.WriteJSON(newErrorMessage(ServerErrorMessage, err))
			conn.Conn.Close()
		} else {
			http.Error(w, err.Error(), 500)
		}
	}
}

func (c *websocketConnection) handshake(w http.ResponseWriter, r *http.Request) error {
	conn, err := c.Server.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return nil
	}
	c.Conn = conn

	err = conn.ReadJSON(&c.AuthData)
	if err != nil {
		c.Close(400, err.Error())
	}

	// Expect auth packet first.
	if c.AuthData.Type() != AuthMessage {
		conn.WriteJSON(newErrorMessage(AuthFailedMessage, errors.New("Auth expected")))
		c.Close(401, "Auth expected")
		return nil
	}

	if c.Server.CanConnect != nil && !c.Server.CanConnect(c.AuthData) {
		conn.WriteJSON(newErrorMessage(AuthFailedMessage, errors.New("Unauthorized")))
		c.Close(401, "Unauthorized")
		return nil
	}

	redis := c.Server.redis
	err = redis.StoreSession(c.Token, c.AuthData)
	if err != nil {
		return err
		conn.WriteJSON(newMessage(ServerErrorMessage))
		conn.Close()
	}

	defer c.Cleanup()

	err = conn.WriteJSON(newMessage(AuthOKMessage))
	if err != nil {
		return err
	}

	hub := c.Server.hub
	err = hub.Connect(c)
	if err != nil {
		return err
	}

	c.Run()

	return nil
}

func (c *websocketConnection) Run() {
	conn := c.Conn
	hub := c.Server.hub

	m := clientMessage{}
	for {
		err := conn.ReadJSON(&m)
		if err != nil {
			c.Close(400, err.Error())
			break
		}

		switch m.Type() {
		case SubscribeMessage:
			channel := m.Channel()
			if c.Server.CanSubscribe != nil && !c.Server.CanSubscribe(c.AuthData, channel) {
				conn.WriteJSON(newChannelErrorMessage(SubscribeErrorMessage, channel, errors.New("Channel refused")))
				continue
			}

			err := hub.Subscribe(c, channel)
			if err != nil {
				conn.WriteJSON(newChannelErrorMessage(SubscribeErrorMessage, channel, err))
			} else {
				conn.WriteJSON(newChannelMessage(SubscribeOKMessage, channel))
			}

		case UnsubscribeMessage:
			channel := m.Channel()

			err := hub.Unsubscribe(c, channel)
			if err != nil {
				conn.WriteJSON(newChannelErrorMessage(UnsubscribeErrorMessage, channel, err))
			}
			conn.WriteJSON(newChannelMessage(UnsubscribeOKMessage, channel))

		case PingMessage:
			// Do nothing

		default:
			conn.WriteJSON(newMessage(UnknownMessage))
			break
		}
	}
}

func (c *websocketConnection) Cleanup() {
	redis := c.Server.redis
	hub := c.Server.hub

	err := redis.DeleteSession(c.Token)
	if err != nil {
		c.Conn.WriteJSON(newErrorMessage(ServerErrorMessage, err))
	}

	err = hub.Disconnect(c)
	if err != nil {
		c.Conn.WriteJSON(newErrorMessage(ServerErrorMessage, err))
	}

	c.Conn.Close()
}

func (c *websocketConnection) Close(code uint16, msg string) {
	payload := make([]byte, 2)
	binary.BigEndian.PutUint16(payload, code)
	payload = append(payload, []byte(msg)...)
	c.Conn.WriteMessage(websocket.CloseMessage, payload)
	c.Conn.Close()
}

func (c *websocketConnection) Send(channel, message string) {
	c.Conn.WriteJSON(newBroadcastMessage(channel, message))
}

func (c *websocketConnection) Process(t string, args []string) {
	panic("Websocket connections don't use control messages!")
}

func (c *websocketConnection) GetToken() string {
	return c.Token
}

// Client transport
type websocketClientTransport struct {
	conn   *websocket.Conn
	client *Client
}

func (t *websocketClientTransport) Connect(authData clientMessage) error {
	conn, _, err := websocket.DefaultDialer.Dial(t.client.url(ClientModeWebsocket), nil)
	if err != nil {
		return err
	}

	t.conn = conn

	// Authenticate
	if !t.client.skip_auth {
		data := authData
		if data == nil {
			data = make(clientMessage)
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
	if t.conn == nil {
		return nil
	}
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
