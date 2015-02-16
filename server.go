package broadcaster

import (
	"net/http"

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

	// PubSub host, used for pubsub, defaults to RedisHost
	PubSubHost string

	hub      hub
	prepared bool
}

const (
	// Client: start authentication
	AuthMessage = "auth"

	// Server: Authentication succeeded
	AuthOKMessage = "authOk"

	// Client: Subscribe to channel
	SubscribeMessage = "subscribe"

	// Server: Subscribe succeeded
	SubscribeOKMessage = "subscribeOk"

	// Server: Subscribe failed
	SubscribeErrorMessage = "subscribeError"

	// Server: Broadcast message
	MessageMessage = "message"
)

type clientMessage map[string]string

func (s *Server) Prepare() error {
	err := s.hub.Prepare(s.RedisHost, s.PubSubHost)
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
	newWebsocketClient(w, r, s)
}

func (s *Server) handleLongPoll(w http.ResponseWriter, r *http.Request) {
}

func (s *Server) Stats() (Stats, error) {
	return s.hub.Stats()
}
