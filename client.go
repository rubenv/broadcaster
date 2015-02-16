package broadcaster

import (
	"fmt"
	"net/url"

	"github.com/gorilla/websocket"
)

// Client connection mode.
type ClientMode int

// Connection modes, can be used to force a specific connection type.
const (
	ClientModeAuto      ClientMode = 0
	ClientModeWebsocket ClientMode = 1
	ClientModeLongPoll  ClientMode = 2
)

type Client struct {
	Mode ClientMode

	// Data passed when authenticating
	AuthData map[string]string

	// Connection params
	host   string
	path   string
	secure bool

	// Only used while testing
	skip_auth bool

	// Websocket connection
	conn *websocket.Conn
}

func NewClient(urlStr string) (*Client, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}

	return &Client{
		host:   u.Host,
		path:   u.Path,
		secure: u.Scheme == "https",
	}, nil
}

func (c *Client) url() string {
	scheme := "ws"

	mode := c.Mode
	if mode == ClientModeAuto {
		mode = ClientModeWebsocket
	}

	if mode == ClientModeLongPoll {
		scheme = "http"
	}
	if c.secure {
		scheme += "s"
	}

	return fmt.Sprintf("%s://%s%s", scheme, c.host, c.path)
}

func (c *Client) Connect() error {
	conn, _, err := websocket.DefaultDialer.Dial(c.url(), nil)
	if err != nil {
		return err
	}

	c.conn = conn

	if c.skip_auth {
		return nil
	}

	// Authenticate
	err = c.send("auth", c.AuthData)
	if err != nil {
		return err
	}

	m, err := c.receive()
	if err != nil {
		return err
	}

	if m["type"] != "authOk" {
		return fmt.Errorf("Expected authOk, got %s instead", m["type"])
	}
	return nil
}

func (c *Client) send(msg string, data map[string]string) error {
	if data == nil {
		data = make(map[string]string)
	}
	data["type"] = msg
	return c.conn.WriteJSON(data)
}

func (c *Client) receive() (clientMessage, error) {
	m := clientMessage{}
	err := c.conn.ReadJSON(&m)
	return m, err
}

func (c *Client) Subscribe(channel string) error {
	err := c.send("subscribe", clientMessage{"channel": channel})
	if err != nil {
		return err
	}

	m, err := c.receive()
	if err != nil {
		return err
	}

	if m["type"] != "subscribeOk" {
		return fmt.Errorf("Expected subscribeOk, got %s instead", m["type"])
	}
	if m["channel"] != channel {
		return fmt.Errorf("Expected channel %s, got %s instead", channel, m["channel"])
	}
	return nil
}

func (c *Client) Unsubscribe(channel string) error {
	err := c.send(UnsubscribeMessage, clientMessage{"channel": channel})
	if err != nil {
		return err
	}

	m, err := c.receive()
	if err != nil {
		return err
	}

	if m["type"] != "unsubscribeOk" {
		return fmt.Errorf("Expected subscribeOk, got %s instead", m["type"])
	}
	if m["channel"] != channel {
		return fmt.Errorf("Expected channel %s, got %s instead", channel, m["channel"])
	}
	return nil
}
