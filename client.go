package broadcaster

import (
	"fmt"
	"net/url"
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

	// Internal bits
	transport clientTransport
	results   map[string]messageChan
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

	// Make sure we have a messages channel
	if c.Messages == nil {
		c.Messages = make(messageChan, 10)
	}

	if !c.skip_auth {
		m, err := c.transport.Receive()
		if err != nil {
			return err
		}

		if m.Type() != AuthOKMessage {
			return fmt.Errorf("Expected authOk, got %s instead", m.Type())
		}
	}
	/*
		// Authenticate
		if !c.skip_auth {
			err := c.send(AuthMessage, c.AuthData)
			if err != nil {
				return err
			}

			m, err := c.receive()
			if err != nil {
				return err
			}

			if m.Type() != AuthOKMessage {
				return fmt.Errorf("Expected authOk, got %s instead", m.Type())
			}
		}
	*/

	go c.listen()

	return nil
}

func (c *Client) Disconnect() error {
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

func (c *Client) listen() {
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
	data["type"] = msg
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

	if m.Type() != SubscribeOKMessage {
		return fmt.Errorf("Expected %s, got %s instead", SubscribeOKMessage, m.Type())
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

	if m.Type() != UnsubscribeOKMessage {
		return fmt.Errorf("Expected %s, got %s instead", UnsubscribeOKMessage, m.Type())
	}
	if m["channel"] != channel {
		return fmt.Errorf("Expected channel %s, got %s instead", channel, m["channel"])
	}
	return nil
}

type clientTransport interface {
	Connect(authData map[string]string) error
	Close() error
	Send(data clientMessage) error
	Receive() (clientMessage, error)
}
