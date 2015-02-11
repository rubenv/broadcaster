package broadcaster

import "net/http"

type Server struct {
	CanConnect func(r *http.Request) bool
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {

}
