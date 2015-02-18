package broadcaster

import "github.com/garyburd/redigo/redis"

type hub struct {
	Running bool

	// Channels
	NewClient        chan connection
	ClientDisconnect chan connection

	Subscribe   chan *subscription
	Unsubscribe chan *subscription

	messages   chan redis.Message
	subscribed chan *subscriptionResult

	subscriptions map[string]map[connection]*subscription

	redis  redis.Conn
	pubSub redis.PubSubConn

	ClientCount int
}

type subscription struct {
	Client  connection
	Channel string
	Done    chan error
}

type subscriptionResult struct {
	Channel string
	Error   error
}

// Server statistics
type Stats struct {
	// Number of active connections
	Connections int

	// For debugging purposes only
	localSubscriptions map[string]int
}

func (h *hub) Prepare(redisHost, pubSubHost string) error {
	if redisHost == "" {
		redisHost = "localhost:6379"
	}
	if pubSubHost == "" {
		pubSubHost = redisHost
	}
	h.NewClient = make(chan connection, 10)
	h.ClientDisconnect = make(chan connection, 10)
	h.Subscribe = make(chan *subscription, 100)
	h.Unsubscribe = make(chan *subscription, 100)

	// Internal channels
	h.messages = make(chan redis.Message, 250)
	h.subscribed = make(chan *subscriptionResult, 5)

	h.subscriptions = make(map[string]map[connection]*subscription)

	r, err := redis.Dial("tcp", redisHost)
	if err != nil {
		return err
	}
	h.redis = r

	p, err := redis.Dial("tcp", pubSubHost)
	if err != nil {
		h.redis.Close()
		return err
	}
	h.pubSub = redis.PubSubConn{Conn: p}

	return nil
}

// Main state machine, ensures that we don't have to care about synchronization.
func (h *hub) Run() {
	go h.receive()

	for {
		select {
		case _ = <-h.NewClient:
			h.ClientCount++
		case _ = <-h.ClientDisconnect:
			h.ClientCount--
		case s := <-h.Subscribe:
			h.handleSubscribe(s)
		case s := <-h.Unsubscribe:
			h.handleUnsubscribe(s)
		case r := <-h.subscribed:
			h.handleSubscribed(r)
		case m := <-h.messages:
			h.handleMessage(m)
		}
	}
}

func (h *hub) receive() {
	for {
		switch v := h.pubSub.Receive().(type) {
		case redis.Message:
			h.messages <- v
		case redis.Subscription:
			h.subscribed <- &subscriptionResult{
				Channel: v.Channel,
				Error:   nil,
			}
		case error:
			// Server stopped?
			return
		}
	}
}

func (h *hub) Stats() (Stats, error) {
	// TODO: Count in Redis
	subscriptions := make(map[string]int)
	for k, v := range h.subscriptions {
		subscriptions[k] = len(v)
	}

	return Stats{
		Connections:        h.ClientCount,
		localSubscriptions: subscriptions,
	}, nil
}

func (h *hub) handleSubscribe(s *subscription) {
	if _, ok := h.subscriptions[s.Channel]; !ok {
		// New channel
		h.subscriptions[s.Channel] = make(map[connection]*subscription)
		err := h.pubSub.Subscribe(s.Channel)
		if err != nil {
			s.Done <- err
		}
	} else {
		s.Done <- nil
	}

	h.subscriptions[s.Channel][s.Client] = s
}

func (h *hub) handleUnsubscribe(s *subscription) {
	delete(h.subscriptions[s.Channel], s.Client)
	if len(h.subscriptions[s.Channel]) == 0 {
		// Channel no longer needed
		h.pubSub.Unsubscribe(s.Channel)
		delete(h.subscriptions, s.Channel)
	}

	s.Done <- nil
}

func (h *hub) handleSubscribed(r *subscriptionResult) {
	for _, subscription := range h.subscriptions[r.Channel] {
		if subscription.Done != nil {
			subscription.Done <- r.Error
			subscription.Done = nil
		}
	}
}

func (h *hub) handleMessage(m redis.Message) {
	for connection, _ := range h.subscriptions[m.Channel] {
		connection.Send(m.Channel, string(m.Data))
	}
}