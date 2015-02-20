package broadcaster

import (
	"errors"
	"fmt"
	"strings"

	"github.com/garyburd/redigo/redis"
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
	h.subscriptions[conn] = make(map[string]bool)
	h.connections[conn.GetToken()] = conn
	return nil
}

func (h *hub) Disconnect(conn connection) error {
	if _, ok := h.subscriptions[conn]; !ok {
		return errors.New("Unknown connection")
	}

	// Unsubscribe from all channels
	for channel, _ := range h.subscriptions[conn] {
		err := h.Unsubscribe(conn, channel)
		if err != nil {
			return err
		}
	}

	delete(h.subscriptions, conn)
	delete(h.connections, conn.GetToken())
	return nil
}

func (h *hub) Subscribe(conn connection, channel string) error {
	if _, ok := h.subscriptions[conn]; !ok {
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
	if _, ok := h.channels[r.Channel]; !ok {
		// New channel! Try to connect to Redis first
		err := h.redis.Subscribe(r.Channel)
		if err != nil {
			r.Done <- err
			return
		}

		h.channels[r.Channel] = make(map[connection]bool)
	}

	h.subscriptions[r.Connection][r.Channel] = true
	h.channels[r.Channel][r.Connection] = true
	r.Done <- nil
}

func (h *hub) Unsubscribe(conn connection, channel string) error {
	if _, ok := h.subscriptions[conn]; !ok {
		return errors.New("Unknown connection")
	}
	if _, ok := h.subscriptions[conn][channel]; !ok {
		return fmt.Errorf("Not subscribed to channel %s", channel)
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
	delete(h.subscriptions[r.Connection], r.Channel)
	delete(h.channels[r.Channel], r.Connection)

	if len(h.channels[r.Channel]) == 0 {
		// Last subscriber, release it.
		err := h.redis.Unsubscribe(r.Channel)
		if err != nil {
			r.Done <- err
			return
		}

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
	subscriptions := make(map[string]int)
	for k, v := range h.channels {
		subscriptions[k] = len(v)
	}

	return hubStats{
		LocalSubscriptions: subscriptions,
	}, nil
}
