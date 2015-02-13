package broadcaster

import "github.com/garyburd/redigo/redis"

type hub struct {
	Running bool

	// Channels
	NewClient        chan client
	ClientDisconnect chan client
	Subscribe        chan subscription

	Subscriptions map[string]map[client]bool

	Redis  redis.Conn
	PubSub redis.PubSubConn

	ClientCount int
}

type subscription struct {
	Client  client
	Channel string
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
	h.Subscribe = make(chan subscription, 100)

	h.Subscriptions = make(map[string]map[client]bool)

	r, err := redis.Dial("tcp", redisHost)
	if err != nil {
		return err
	}
	h.Redis = r

	p, err := redis.Dial("tcp", pubSubHost)
	if err != nil {
		h.Redis.Close()
		return err
	}
	h.PubSub = redis.PubSubConn{Conn: p}

	return nil
}

func (h *hub) Run() {
	for {
		select {
		case _ = <-h.NewClient:
			h.ClientCount++
		case _ = <-h.ClientDisconnect:
			h.ClientCount--
		case s := <-h.Subscribe:
			if _, ok := h.Subscriptions[s.Channel]; !ok {
				// TODO: Connect to redis
				// New channel
				h.Subscriptions[s.Channel] = make(map[client]bool)
			}

			h.Subscriptions[s.Channel][s.Client] = true
		}
	}
}

func (h *hub) Stats() (Stats, error) {
	// TODO: Count in Redis
	subscriptions := make(map[string]int)
	for k, v := range h.Subscriptions {
		subscriptions[k] = len(v)
	}

	return Stats{
		Connections:        h.ClientCount,
		localSubscriptions: subscriptions,
	}, nil
}
