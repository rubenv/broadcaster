package broadcaster

import (
	"encoding/binary"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pborman/uuid"
	"github.com/uber-go/atomic"
)

type websocketConnection struct {
	Token    string
	Conn     *websocket.Conn
	Server   *Server
	AuthData ClientMessage

	write_lock sync.Mutex
	read_lock  sync.Mutex
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

func (c *websocketConnection) writeConn(msg ClientMessage) error {
	c.write_lock.Lock()
	defer c.write_lock.Unlock()
	return c.Conn.WriteJSON(msg)
}

func (c *websocketConnection) readConn(v interface{}) error {
	c.read_lock.Lock()
	defer c.read_lock.Unlock()
	return c.Conn.ReadJSON(v)
}

func (c *websocketConnection) handshake(w http.ResponseWriter, r *http.Request) error {
	conn, err := c.Server.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		// websocket library already sends error message, nothing to do here
		return nil
	}
	c.Conn = conn

	err = c.readConn(&c.AuthData)
	if err != nil {
		c.Close(4400, err.Error())
		return nil
	}

	// Expect auth packet first.
	if c.AuthData.Type() != AuthMessage {
		c.writeConn(newErrorMessage(AuthFailedMessage, errors.New("Auth expected")))
		c.Close(4401, "Auth expected")
		return nil
	}

	if c.Server.CanConnect != nil && !c.Server.CanConnect(c.AuthData) {
		c.writeConn(newErrorMessage(AuthFailedMessage, errors.New("Unauthorized")))
		c.Close(4401, "Unauthorized")
		return nil
	}

	redis := c.Server.redis
	err = redis.StoreSession(c.Token, c.AuthData)
	if err != nil {
		c.writeConn(newMessage(ServerErrorMessage))
		conn.Close()
		return nil
	}

	defer c.Cleanup()

	err = c.writeConn(newMessage(AuthOKMessage))
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
	hub := c.Server.hub

	m := ClientMessage{}
	for {
		err := c.readConn(&m)
		if err != nil {
			c.Close(4400, err.Error())
			break
		}

		switch m.Type() {
		case SubscribeMessage:
			channel := m.Channel()
			if c.Server.CanSubscribe != nil && !c.Server.CanSubscribe(c.AuthData, channel) {
				c.writeConn(newChannelErrorMessage(SubscribeErrorMessage, channel, errors.New("Channel refused")))
				continue
			}

			err := hub.Subscribe(c, channel)
			if err != nil {
				c.writeConn(newChannelErrorMessage(SubscribeErrorMessage, channel, err))
			} else {
				c.writeConn(newChannelMessage(SubscribeOKMessage, channel))
			}

		case UnsubscribeMessage:
			channel := m.Channel()

			err := hub.Unsubscribe(c, channel)
			if err != nil {
				c.writeConn(newChannelErrorMessage(UnsubscribeErrorMessage, channel, err))
				continue
			}
			c.writeConn(newChannelMessage(UnsubscribeOKMessage, channel))

		case PingMessage:
			// Do nothing

		default:
			c.writeConn(newMessage(UnknownMessage))
		}
	}
}

func (c *websocketConnection) Cleanup() {
	redis := c.Server.redis
	hub := c.Server.hub

	err := redis.DeleteSession(c.Token)
	if err != nil {
		c.writeConn(newErrorMessage(ServerErrorMessage, err))
	}

	err = hub.Disconnect(c)
	if err != nil {
		c.writeConn(newErrorMessage(ServerErrorMessage, err))
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
	c.writeConn(newBroadcastMessage(channel, message))
}

func (c *websocketConnection) Process(t string, args []string) {
	panic("Websocket connections don't use control messages!")
}

func (c *websocketConnection) GetToken() string {
	return c.Token
}

// Client transport
type websocketClientTransport struct {
	conn      *websocket.Conn
	conn_lock sync.Mutex
	client    *Client
	running   *atomic.Bool

	write_lock sync.Mutex
	read_lock  sync.Mutex
}

func newWebsocketClientTransport(c *Client) clientTransport {
	return &websocketClientTransport{
		running: atomic.NewBool(false),
		client:  c,
	}
}

func (t *websocketClientTransport) writeConn(msg ClientMessage) error {
	t.write_lock.Lock()
	defer t.write_lock.Unlock()
	return t.conn.WriteJSON(msg)
}

func (t *websocketClientTransport) readConn(v interface{}) error {
	t.read_lock.Lock()
	defer t.read_lock.Unlock()
	return t.conn.ReadJSON(v)
}

func (t *websocketClientTransport) Connect(authData ClientMessage) error {
	var header http.Header = nil
	if t.client.UserAgent != "" {
		header = make(http.Header)
		header.Set("User-Agent", t.client.UserAgent)
	}
	conn, _, err := websocket.DefaultDialer.Dial(t.client.url(ClientModeWebsocket), header)
	if err != nil {
		return err
	}

	t.conn_lock.Lock()
	t.conn = conn
	t.conn_lock.Unlock()

	// Authenticate
	if !t.client.skip_auth {
		data := authData
		if data == nil {
			data = make(ClientMessage)
		}
		data["__type"] = AuthMessage
		err := t.Send(data)
		if err != nil {
			return err
		}
	}

	t.running.Store(true)
	go func() {
		for {
			time.Sleep(t.client.PingInterval)
			if !t.running.Load() {
				return
			}
			t.Send(newMessage(PingMessage))
		}
	}()

	return nil
}

func (t *websocketClientTransport) Close() error {
	t.conn_lock.Lock()
	defer t.conn_lock.Unlock()

	t.running.Store(false)
	if t.conn == nil {
		return nil
	}
	return t.conn.Close()
}

func (t *websocketClientTransport) Send(data ClientMessage) error {
	return t.writeConn(data)
}

func (t *websocketClientTransport) Receive() (ClientMessage, error) {
	m := ClientMessage{}
	err := t.readConn(&m)
	return m, err
}

func (t *websocketClientTransport) onConnect() {
}
