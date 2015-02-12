package broadcaster

import (
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

type Server struct {
	CanConnect func(r *http.Request) bool

	// Can be used to configure buffer sizes etc.
	//
	// See http://godoc.org/github.com/gorilla/websocket#Upgrader
	Upgrader websocket.Upgrader
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_, err := s.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
}
