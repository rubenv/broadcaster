package broadcaster

import "testing"

func TestLPConnect(t *testing.T) {
	server, err := startServer(nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	_, err = newLPClient(server)
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
