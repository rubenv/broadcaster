package broadcaster

import (
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// A Server is the main class of this package, pass it to http.Handle on a
// chosen path to start a broadcast server.
type Server struct {
	// Invoked upon initial connection, can be used to enforce access control.
	CanConnect func(data map[string]string) bool

	// Invoked upon channel subscription, can be used to enforce access control
	// for channels.
	CanSubscribe func(data map[string]string, channel string) bool

	// Can be used to configure buffer sizes etc.
	// See http://godoc.org/github.com/gorilla/websocket#Upgrader
	Upgrader websocket.Upgrader

	// Redis host, used for data, defaults to localhost:6379
	RedisHost string

	// Redis pubsub channel, used for internal coordination messages
	// Defaults to "broadcaster"
	ControlChannel string

	// Namespace for storing session data.
	// Defaults to "bc:"
	ControlNamespace string

	// PubSub host, used for pubsub, defaults to RedisHost
	PubSubHost string

	// Timeout for long-polling connections
	Timeout time.Duration

	// Combine long poll message for given duration (more latency, less load)
	PollTime time.Duration

	redis    *redisBackend
	hub      *hub
	prepared bool
}

func (s *Server) Prepare() error {
	// Defaults
	if s.RedisHost == "" {
		s.RedisHost = "localhost:6379"
	}
	if s.PubSubHost == "" {
		s.PubSubHost = s.RedisHost
	}
	if s.ControlChannel == "" {
		s.ControlChannel = "broadcaster"
	}
	if s.ControlNamespace == "" {
		s.ControlNamespace = "bc:"
	}
	if s.Timeout == 0 {
		s.Timeout = 30 * time.Second
	}
	if s.PollTime == 0 {
		s.PollTime = 2 * time.Second
	}

	redis, err := newRedisBackend(s.RedisHost, s.PubSubHost, s.ControlChannel, s.ControlNamespace, s.Timeout)
	if err != nil {
		return err
	}
	s.redis = redis

	s.hub = &hub{
		redis: redis,
	}

	err = s.hub.Prepare()
	if err != nil {
		return err
	}

	go s.hub.Run()
	s.prepared = true
	return nil
}

// Main HTTP server.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.prepared {
		http.Error(w, "Prepare() not called on broadcaster.Server", 500)
		return
	}

	if r.Method == "GET" {
		s.handleWebsocket(w, r)
	} else if r.Method == "POST" {
		s.handleLongPoll(w, r)
	}
}

func (s *Server) handleWebsocket(w http.ResponseWriter, r *http.Request) {
	// Always a new client, easy!
	newWebsocketConnection(w, r, s)
}

func (s *Server) handleLongPoll(w http.ResponseWriter, r *http.Request) {
	err := handleLongpollConnection(w, r, s)
	if err != nil {
		http.Error(w, err.Error(), 500)
	}
}

type Stats struct {
	// Number of active connections
	Connections int

	// For debugging purposes only
	LocalSubscriptions map[string]int
}

func (s *Server) Stats() (Stats, error) {
	hubStats, err := s.hub.Stats()
	if err != nil {
		return Stats{}, err
	}

	connected, err := s.redis.GetConnected()
	if err != nil {
		return Stats{}, err
	}

	stats := Stats{
		Connections:        connected,
		LocalSubscriptions: hubStats.LocalSubscriptions,
	}

	return stats, nil
}
