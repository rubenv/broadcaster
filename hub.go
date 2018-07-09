package broadcaster

import (
	"errors"
	"strings"
	"sync"

	"github.com/gomodule/redigo/redis"
)

type connection interface {
	Send(channel, message string)
	Process(t string, args []string)
	GetToken() string
}

type subscriptionRequest struct {
	Connection connection
	Channel    string
	Done       chan error
}

type hub struct {
	quit chan struct{}

	redis *redisBackend

	// Keeps track of all channels a connection is subscribed to.
	subscriptions map[connection]map[string]bool

	// Allows mapping channels to subscribers.
	channels map[string]map[connection]bool

	// Makes tokens to connections
	connections map[string]connection

	newSubscriptions   chan subscriptionRequest
	newUnsubscriptions chan subscriptionRequest

	sync.Mutex
}

func (h *hub) Prepare() error {
	h.quit = make(chan struct{})

	h.subscriptions = make(map[connection]map[string]bool)
	h.channels = make(map[string]map[connection]bool)
	h.connections = make(map[string]connection)

	h.newSubscriptions = make(chan subscriptionRequest, 100)
	h.newUnsubscriptions = make(chan subscriptionRequest, 100)

	return nil
}

func (h *hub) Run() {
	for {
		select {
		case r := <-h.newSubscriptions:
			h.handleSubscribe(r)
		case r := <-h.newUnsubscriptions:
			h.handleUnsubscribe(r)
		case m := <-h.redis.Messages:
			h.handleMessage(m)
		case <-h.quit:
			return
		}
	}
}

func (h *hub) Stop() {
	h.quit <- struct{}{}
}

func (h *hub) Connect(conn connection) error {
	h.Lock()
	defer h.Unlock()

	h.subscriptions[conn] = make(map[string]bool)
	h.connections[conn.GetToken()] = conn
	return nil
}

func (h *hub) Disconnect(conn connection) error {
	if !h.hasConnection(conn) {
		return errors.New("Unknown connection")
	}

	h.Lock()
	channels := make([]string, 0)
	for channel, _ := range h.subscriptions[conn] {
		channels = append(channels, channel)
	}
	h.Unlock()

	// Unsubscribe from all channels
	for _, channel := range channels {
		err := h.Unsubscribe(conn, channel)
		if err != nil {
			return err
		}
	}

	h.Lock()
	defer h.Unlock()
	delete(h.subscriptions, conn)
	delete(h.connections, conn.GetToken())
	return nil
}

func (h *hub) hasConnection(conn connection) bool {
	h.Lock()
	defer h.Unlock()

	_, ok := h.subscriptions[conn]
	return ok
}

func (h *hub) hasSubscription(conn connection, channel string) bool {
	h.Lock()
	defer h.Unlock()

	s, ok := h.subscriptions[conn]
	if !ok {
		return false
	}

	_, ok = s[channel]
	return ok
}

func (h *hub) Subscribe(conn connection, channel string) error {
	if !h.hasConnection(conn) {
		return errors.New("Unknown connection")
	}

	r := subscriptionRequest{
		Connection: conn,
		Channel:    channel,
		Done:       make(chan error),
	}
	h.newSubscriptions <- r
	return <-r.Done
}

func (h *hub) handleSubscribe(r subscriptionRequest) {
	h.Lock()
	defer h.Unlock()

	if _, ok := h.channels[r.Channel]; !ok {
		// New channel! Try to connect to Redis first
		h.redis.Subscribe(r.Channel)
		h.channels[r.Channel] = make(map[connection]bool)
	}

	h.subscriptions[r.Connection][r.Channel] = true
	h.channels[r.Channel][r.Connection] = true
	r.Done <- nil
}

func (h *hub) Unsubscribe(conn connection, channel string) error {
	if !h.hasConnection(conn) {
		return errors.New("Unknown connection")
	}
	if !h.hasSubscription(conn, channel) {
		// Some clients seem to be sending double unsubscribes,
		// ignore those for now:
		//return fmt.Errorf("Not subscribed to channel %s", channel)
		return nil
	}

	r := subscriptionRequest{
		Connection: conn,
		Channel:    channel,
		Done:       make(chan error),
	}
	h.newUnsubscriptions <- r
	return <-r.Done
}

func (h *hub) handleUnsubscribe(r subscriptionRequest) {
	h.Lock()
	defer h.Unlock()

	delete(h.subscriptions[r.Connection], r.Channel)
	delete(h.channels[r.Channel], r.Connection)

	if len(h.channels[r.Channel]) == 0 {
		// Last subscriber, release it.
		h.redis.Unsubscribe(r.Channel)
		delete(h.channels, r.Channel)
	}

	r.Done <- nil
}

func (h *hub) processClient(t, token string, args []string) {
	if c, ok := h.connections[token]; ok {
		c.Process(t, args)
	}
}

func (h *hub) handleMessage(m redis.Message) {
	h.Lock()
	defer h.Unlock()

	if m.Channel == h.redis.controlChannel {
		args := strings.Split(string(m.Data), " ")
		switch args[0] {
		case "transfer":
			h.processClient(args[0], args[1], args[2:])
		case "subscribe":
			h.processClient(args[0], args[1], args[2:])
		case "unsubscribe":
			h.processClient(args[0], args[1], args[2:])
		}
	} else {
		if _, ok := h.channels[m.Channel]; !ok {
			return // No longer subscribed?
		}

		for conn, _ := range h.channels[m.Channel] {
			conn.Send(m.Channel, string(m.Data))
		}
	}
}

type hubStats struct {
	LocalSubscriptions map[string]int
}

func (h *hub) Stats() (hubStats, error) {
	h.Lock()
	defer h.Unlock()

	subscriptions := make(map[string]int)
	for k, v := range h.channels {
		subscriptions[k] = len(v)
	}

	return hubStats{
		LocalSubscriptions: subscriptions,
	}, nil
}
