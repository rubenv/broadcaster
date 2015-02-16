package broadcaster

import "github.com/garyburd/redigo/redis"

type hub struct {
	Running bool

	// Channels
	NewClient        chan client
	ClientDisconnect chan client

	Subscribe chan *subscription

	messages   chan redis.Message
	subscribed chan *subscriptionResult

	subscriptions map[string]map[client]*subscription

	redis  redis.Conn
	pubSub redis.PubSubConn

	ClientCount int
}

type subscription struct {
	Client  client
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
	h.NewClient = make(chan client, 10)
	h.ClientDisconnect = make(chan client, 10)
	h.Subscribe = make(chan *subscription, 100)

	// Internal channels
	h.messages = make(chan redis.Message, 250)
	h.subscribed = make(chan *subscriptionResult, 5)

	h.subscriptions = make(map[string]map[client]*subscription)

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

func (h *hub) Run() {
	go h.receive()

	for {
		select {
		case _ = <-h.NewClient:
			h.ClientCount++
		case _ = <-h.ClientDisconnect:
			h.ClientCount--
		case s := <-h.Subscribe:
			if _, ok := h.subscriptions[s.Channel]; !ok {
				// New channel
				h.subscriptions[s.Channel] = make(map[client]*subscription)
				err := h.pubSub.Subscribe(s.Channel)
				if err != nil {
					s.Done <- err
				}
			} else {
				s.Done <- nil
			}

			h.subscriptions[s.Channel][s.Client] = s
		case r := <-h.subscribed:
			for _, subscription := range h.subscriptions[r.Channel] {
				if subscription.Done != nil {
					subscription.Done <- r.Error
					subscription.Done = nil
				}
			}
		case m := <-h.messages:
			for client, _ := range h.subscriptions[m.Channel] {
				client.Send(m.Channel, string(m.Data))
			}
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
