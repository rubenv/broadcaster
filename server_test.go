package broadcaster

import "testing"

func TestConnect(t *testing.T) {
	server, err := startServer(nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	client, err := newWSClient(server)
	if err != nil {
		t.Fatal(err)
	}

	err = client.Authenticate(nil)
	if err != nil {
		t.Fatal(err)
	}

	stats, err := server.Broadcaster.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.Connections != 1 {
		t.Errorf("Unexpected connection count: %d", stats.Connections)
	}
}

func TestCanConnect(t *testing.T) {
	server, err := startServer(&Server{
		CanConnect: func(data map[string]string) bool {
			return false
		},
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	client, err := newWSClient(server)
	if err != nil {
		t.Fatal(err)
	}

	err = client.Authenticate(nil)
	if err == nil {
		t.Fatal("Expected error!")
	}
	if err.Error() != "websocket: close 401 Unauthorized" {
		t.Fatal("Did not properly deny access")
	}

	stats, err := server.Broadcaster.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.Connections != 0 {
		t.Errorf("Unexpected connection count: %d", stats.Connections)
	}
}

func TestRefusesUnauthedCommands(t *testing.T) {

}
