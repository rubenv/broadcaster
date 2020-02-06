package broadcaster

import (
	"errors"
	"fmt"
	"net/url"
	"sync"
	"time"

	"go.uber.org/atomic"
)

// Client connection mode.
type ClientMode int

// Connection modes, can be used to force a specific connection type.
const (
	ClientModeAuto      ClientMode = 0
	ClientModeWebsocket ClientMode = 1
	ClientModeLongPoll  ClientMode = 2
)

type messageChan chan ClientMessage

type Client struct {
	Mode ClientMode

	// Data passed when authenticating
	AuthData map[string]interface{}

	// Set when disconnecting
	Error error

	// Incoming messages
	Messages chan ClientMessage

	// Receives true when disconnected
	Disconnected chan bool

	// Timeout
	Timeout time.Duration

	// Ping interval
	PingInterval time.Duration

	// Reconnection attempts
	MaxAttempts int

	// Can be overwritten
	UserAgent string

	// Connection params
	host   string
	path   string
	secure bool

	// Only used while testing
	skip_auth bool

	// Internal bits
	transport         clientTransport
	results           map[string]messageChan
	results_lock      sync.Mutex
	should_disconnect *atomic.Bool
	attempts          int

	channels      map[string]bool
	channels_lock sync.Mutex

	disconnect_lock sync.Mutex
	disconnect_done bool
}

func NewClient(urlStr string) (*Client, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}

	return &Client{
		host:              u.Host,
		path:              u.Path,
		secure:            u.Scheme == "https",
		Timeout:           30 * time.Second,
		PingInterval:      30 * time.Second,
		MaxAttempts:       10,
		channels:          make(map[string]bool),
		results:           make(map[string]messageChan),
		Messages:          make(messageChan, 10),
		Disconnected:      make(chan bool),
		should_disconnect: atomic.NewBool(false),
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
	c.should_disconnect.Store(false)

	if c.Mode == ClientModeAuto || c.Mode == ClientModeWebsocket {
		c.transport = newWebsocketClientTransport(c)
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

	c.channels_lock.Lock()
	toSubscribe := make([]string, 0)
	for channel, _ := range c.channels {
		toSubscribe = append(toSubscribe, channel)
	}
	c.channels_lock.Unlock()

	for _, channel := range toSubscribe {
		err := c.Subscribe(channel)
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *Client) Disconnect() error {
	c.should_disconnect.Store(true)

	c.disconnect_lock.Lock()
	defer c.disconnect_lock.Unlock()

	if c.disconnect_done {
		return nil
	}

	err := c.transport.Close()
	if err != nil && c.Error == nil {
		c.Error = err
	}
	c.results_lock.Lock()
	for _, r := range c.results {
		close(r)
	}
	c.results_lock.Unlock()

	close(c.Messages)
	c.disconnect_done = true

	return c.Error
}

func (c *Client) disconnected() {
	should_disconnect := c.should_disconnect.Load()
	if should_disconnect {
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
			c.disconnected()
			return
		}

		if m.Type() == MessageMessage {
			c.relay(m)
		} else {
			c.results_lock.Lock()
			channel, ok := c.results[m.ResultId()]
			c.results_lock.Unlock()
			if !ok {
				// Unrequested result?
			} else {
				channel <- m
			}
		}
	}
}

// Prevents sending messages after we've disconnected
func (c *Client) relay(m ClientMessage) {
	c.disconnect_lock.Lock()
	defer c.disconnect_lock.Unlock()

	if !c.disconnect_done {
		c.Messages <- m
	}
}

func (c *Client) send(msg string, data ClientMessage) error {
	if data == nil {
		data = make(ClientMessage)
	}
	data["__type"] = msg
	return c.transport.Send(data)
}

func (c *Client) receive() (ClientMessage, error) {
	return c.transport.Receive()
}

func (c *Client) resultChan(format string, args ...interface{}) chan ClientMessage {
	name := fmt.Sprintf(format, args...)
	channel := make(chan ClientMessage, 1)
	c.results_lock.Lock()
	c.results[name] = channel
	c.results_lock.Unlock()
	return channel
}

func (c *Client) call(msgType string, msg ClientMessage) (ClientMessage, error) {
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
	m, err := c.call(SubscribeMessage, ClientMessage{"channel": channel})
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

	c.channels_lock.Lock()
	c.channels[channel] = true
	c.channels_lock.Unlock()
	return nil
}

func (c *Client) Unsubscribe(channel string) error {
	m, err := c.call(UnsubscribeMessage, ClientMessage{"channel": channel})
	if err != nil {
		return err
	}

	if m.Type() != UnsubscribeOKMessage {
		return fmt.Errorf("Expected %s, got %s instead", UnsubscribeOKMessage, m.Type())
	}
	if m["channel"] != channel {
		return fmt.Errorf("Expected channel %s, got %s instead", channel, m["channel"])
	}

	c.channels_lock.Lock()
	c.channels[channel] = false
	c.channels_lock.Unlock()
	return nil
}

type clientTransport interface {
	Connect(authData ClientMessage) error
	Close() error
	Send(data ClientMessage) error
	Receive() (ClientMessage, error)

	onConnect()
}
