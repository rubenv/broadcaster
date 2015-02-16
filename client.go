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

type messageChan chan clientMessage

type Client struct {
	Mode ClientMode

	// Data passed when authenticating
	AuthData map[string]string

	// Set when disconnecting
	Error error

	// Incoming messages
	Messages chan clientMessage

	// Connection params
	host   string
	path   string
	secure bool

	// Only used while testing
	skip_auth bool

	// Websocket connection
	conn *websocket.Conn

	results map[string]messageChan
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

	// Make sure we have a messages channel
	if c.Messages == nil {
		c.Messages = make(messageChan, 10)
	}

	// Authenticate
	if !c.skip_auth {
		err = c.send(AuthMessage, c.AuthData)
		if err != nil {
			return err
		}

		m, err := c.receive()
		if err != nil {
			return err
		}

		if m["type"] != AuthOKMessage {
			return fmt.Errorf("Expected authOk, got %s instead", m["type"])
		}
	}

	go c.listen()

	return nil
}

func (c *Client) Disconnect() error {
	close(c.Messages)
	for _, r := range c.results {
		close(r)
	}
	err := c.conn.Close()
	if err != nil && c.Error == nil {
		c.Error = err
	}
	return c.Error
}

func (c *Client) listen() {
	for {
		m, err := c.receive()
		if err != nil {
			c.Error = err
			c.Disconnect()
			return
		}

		if m["type"] == MessageMessage {
			c.Messages <- m
		} else {
			channel, ok := c.results[m.ResultId()]
			if !ok {
				// Unrequested result?
			} else {
				channel <- m
			}
		}
	}
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

func (c *Client) resultChan(format string, args ...interface{}) chan clientMessage {
	if c.results == nil {
		c.results = make(map[string]messageChan)
	}
	name := fmt.Sprintf(format, args...)
	channel := make(chan clientMessage, 1)
	c.results[name] = channel
	return channel
}

func (c *Client) call(msgType string, msg clientMessage) (clientMessage, error) {
	result := c.resultChan("%s_%s", msgType, msg["channel"])

	err := c.send(msgType, msg)
	if err != nil {
		return nil, err
	}

	m, ok := <-result
	if !ok {
		return nil, c.Error
	}
	return m, nil
}

func (c *Client) Subscribe(channel string) error {
	m, err := c.call(SubscribeMessage, clientMessage{"channel": channel})
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
	m, err := c.call(UnsubscribeMessage, clientMessage{"channel": channel})
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
