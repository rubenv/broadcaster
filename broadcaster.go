package broadcaster

import (
	"net/http"

	"github.com/gorilla/websocket"
)

// Broadcast server
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
}

type Connection struct {
	socket *websocket.Conn
}

// Server statistics
type Stats struct {
	// Number of active websocket connections (note: does not include long-polling connections)
	Connections int
}

// Main HTTP server.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.CanConnect != nil && !s.CanConnect(r) {
		http.Error(w, "Unauthorized", 401)
		return
	}

	if r.Method == "GET" {
		s.handleWebsocket(w, r)
	}

	// TODO: Long-polling support
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

// Retrieve server stats
func (s *Server) Stats() Stats {
	return Stats{
		Connections: len(s.connections),
	}
}
