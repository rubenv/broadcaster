package broadcaster

import (
	"errors"
	"fmt"
	"net/url"
	"time"
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
	AuthData map[string]interface{}

	// Set when disconnecting
	Error error

	// Incoming messages
	Messages chan clientMessage

	// Receives true when disconnected
	Disconnected chan bool

	// Timeout
	Timeout time.Duration

	// Reconnection attempts
	MaxAttempts int

	// Connection params
	host   string
	path   string
	secure bool

	// Only used while testing
	skip_auth bool

	// Internal bits
	transport         clientTransport
	results           map[string]messageChan
	should_disconnect bool
	attempts          int
	channels          map[string]bool
}

func NewClient(urlStr string) (*Client, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}

	return &Client{
		host:         u.Host,
		path:         u.Path,
		secure:       u.Scheme == "https",
		Timeout:      30 * time.Second,
		MaxAttempts:  10,
		channels:     make(map[string]bool),
		Messages:     make(messageChan, 10),
		Disconnected: make(chan bool, 0),
	}, nil
}

func (c *Client) url(mode ClientMode) string {
	scheme := "ws"

	if mode == ClientModeLongPoll {
		scheme = "http"
	}

	if c.secure {
		scheme += "s"
	}

	return fmt.Sprintf("%s://%s%s", scheme, c.host, c.path)
}

func (c *Client) Connect() error {
	c.should_disconnect = false

	if c.Mode == ClientModeAuto || c.Mode == ClientModeWebsocket {
		c.transport = &websocketClientTransport{client: c}
		err := c.transport.Connect(c.AuthData)
		if err != nil {
			if c.Mode == ClientModeAuto {
				c.transport = newlongpollClientTransport(c)
				err := c.transport.Connect(c.AuthData)
				if err != nil {
					return err
				}
			} else {
				return err
			}
		}
	} else if c.Mode == ClientModeLongPoll {
		c.transport = newlongpollClientTransport(c)
		err := c.transport.Connect(c.AuthData)
		if err != nil {
			return err
		}
	} else {
		return fmt.Errorf("Unknown client mode: %d", c.Mode)
	}

	if !c.skip_auth {
		m, err := c.transport.Receive()
		if err != nil {
			return err
		}

		if m.Type() == AuthFailedMessage {
			return fmt.Errorf("Auth error: %s", m["reason"])
		} else if m.Type() != AuthOKMessage {
			return fmt.Errorf("Expected %s or %s, got %s instead", AuthOKMessage, AuthFailedMessage, m.Type())
		}
	}

	go c.listen()

	for channel, _ := range c.channels {
		err := c.Subscribe(channel)
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *Client) Disconnect() error {
	c.should_disconnect = true
	close(c.Messages)
	for _, r := range c.results {
		close(r)
	}
	err := c.transport.Close()
	if err != nil && c.Error == nil {
		c.Error = err
	}
	return c.Error
}

func (c *Client) disconnected() {
	if c.should_disconnect {
		return
	}

	if c.attempts == c.MaxAttempts {
		c.Error = errors.New("Disconnected")
		c.Disconnected <- true
	}

	c.attempts++
	err := c.Connect()
	if err == nil {
		// Connected!
		return
	}

	// Back off
	<-time.After(time.Duration(c.attempts-1) * time.Second)
	c.disconnected()
}

func (c *Client) listen() {
	c.transport.onConnect()

	for {
		m, err := c.receive()
		if err != nil {
			c.Error = err
			c.Disconnect()
			return
		}

		if m.Type() == MessageMessage {
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

func (c *Client) send(msg string, data clientMessage) error {
	if data == nil {
		data = make(clientMessage)
	}
	data["__type"] = msg
	return c.transport.Send(data)
}

func (c *Client) receive() (clientMessage, error) {
	return c.transport.Receive()
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

	if m.Type() == SubscribeErrorMessage {
		return fmt.Errorf("Subscribe error: %s", m["reason"])
	} else if m.Type() != SubscribeOKMessage {
		return fmt.Errorf("Expected %s or %s, got %s instead", SubscribeOKMessage, SubscribeErrorMessage, m.Type())
	}

	if m["channel"] != channel {
		return fmt.Errorf("Expected channel %s, got %s instead", channel, m["channel"])
	}
	c.channels[channel] = true
	return nil
}

func (c *Client) Unsubscribe(channel string) error {
	m, err := c.call(UnsubscribeMessage, clientMessage{"channel": channel})
	if err != nil {
		return err
	}

	if m.Type() != UnsubscribeOKMessage {
		return fmt.Errorf("Expected %s, got %s instead", UnsubscribeOKMessage, m.Type())
	}
	if m["channel"] != channel {
		return fmt.Errorf("Expected channel %s, got %s instead", channel, m["channel"])
	}
	c.channels[channel] = false
	return nil
}

type clientTransport interface {
	Connect(authData clientMessage) error
	Close() error
	Send(data clientMessage) error
	Receive() (clientMessage, error)

	onConnect()
}
