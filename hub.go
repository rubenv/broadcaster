package broadcaster

import "github.com/garyburd/redigo/redis"

type hub struct {
	Running bool
	Server  *Server

	NewClient chan client

	Redis redis.Conn

	ClientCount int
}

// Server statistics
type Stats struct {
	// Number of active connections
	Connections int
}

func (h *hub) Run() {
	h.NewClient = make(chan client, 10)

	//exit := false

	for {
		select {
		case _ = <-h.NewClient:
			h.ClientCount++
		}
	}
}

func (h *hub) Stats() (Stats, error) {
	return Stats{
		// TODO: Count in Redis
		Connections: h.ClientCount,
	}, nil
}
