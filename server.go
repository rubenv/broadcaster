package broadcaster

import (
	"net/http"

	"github.com/gorilla/websocket"
)

// A Server is the main class of this package, pass it to http.Handle on a
// chosen path to start a broadcast server.
type Server struct {
	// Invoked upon initial connection, can be used to enforce access control.
	CanConnect func(r *http.Request) bool

	// Invoked upon channel subscription, can be used to enforce access control
	// for channels.
	CanSubscribe func(c *Connection, channel string) bool

	// Can be used to configure buffer sizes etc.
	// See http://godoc.org/github.com/gorilla/websocket#Upgrader
	Upgrader websocket.Upgrader

	// Local (websocket) connections
	connections []*Connection

	hub hub
}

type Connection struct {
	authenticated bool
	socket        *websocket.Conn
}

type clientMessage struct {
	Id   string            `json:"id"`
	Data map[string]string `json:"data"`
}

// Server statistics
type Stats struct {
	// Number of active websocket connections (note: does not include
	// long-polling connections)
	Connections int
}

// Main HTTP server.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Start hub if it's not already running
	if !s.hub.Running {
		s.hub.Start()
	}

	if s.CanConnect != nil && !s.CanConnect(r) {
		http.Error(w, "Unauthorized", 401)
		return
	}

	if r.Method == "GET" {
		s.handleWebsocket(w, r)
	} else if r.Method == "POST" {
		s.handleLongPoll(w, r)
	}
}

func (s *Server) handleWebsocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	// TODO: Make thread-safe
	s.connections = append(s.connections, &Connection{
		socket: conn,
	})
}

func (s *Server) handleLongPoll(w http.ResponseWriter, r *http.Request) {
}

// Retrieve server stats
func (s *Server) Stats() Stats {
	return Stats{
		// TODO: Count in Redis
		Connections: len(s.connections),
	}
}
