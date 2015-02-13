package broadcaster

import (
	"net/http"
	"testing"
)

func TestConnect(t *testing.T) {
	server, err := startServer(nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	_, err = newClient(server)
	if err != nil {
		t.Fatal(err)
	}

	stats := server.Broadcaster.Stats()
	if stats.Connections != 1 {
		t.Errorf("Unexpected connection count: %d", stats.Connections)
	}
}

func TestCanConnect(t *testing.T) {
	server, err := startServer(&Server{
		CanConnect: func(r *http.Request) bool {
			return false
		},
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	_, err = newClient(server)
	cerr := err.(clientError)
	if err == nil || cerr.Response.StatusCode != 401 {
		t.Fatal("Did properly deny access")
	}

	stats := server.Broadcaster.Stats()
	if stats.Connections != 0 {
		t.Errorf("Unexpected connection count: %d", stats.Connections)
	}
}

func TestRefusesUnauthedCommands(t *testing.T) {

}
