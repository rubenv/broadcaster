package broadcaster

import "github.com/garyburd/redigo/redis"

type hub struct {
	Running bool
	Server  *Server

	NewClient        chan client
	ClientDisconnect chan client

	Subscribe chan client

	Redis redis.Conn

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
}

func (h *hub) Run() {
	h.NewClient = make(chan client, 10)
	h.ClientDisconnect = make(chan client, 10)
	h.Subscribe = make(chan client, 100)

	for {
		select {
		case _ = <-h.NewClient:
			h.ClientCount++
		case _ = <-h.ClientDisconnect:
			h.ClientCount--
		}
	}
}

func (h *hub) Stats() (Stats, error) {
	return Stats{
		// TODO: Count in Redis
		Connections: h.ClientCount,
	}, nil
}
