package broadcaster

import "testing"

func testConnect(t *testing.T, clientFn func(s *testServer, conf ...func(c *Client)) (*Client, error)) {
	server, err := startServer(nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	_, err = clientFn(server)
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

func testCanConnect(t *testing.T, clientFn func(s *testServer, conf ...func(c *Client)) (*Client, error)) {
	server, err := startServer(&Server{
		CanConnect: func(data map[string]string) bool {
			return false
		},
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	_, err = clientFn(server)
	if err == nil || err.Error() != "Auth error: Unauthorized" {
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

func testAuthData(t *testing.T, clientFn func(s *testServer, conf ...func(c *Client)) (*Client, error)) {
	server, err := startServer(&Server{
		CanConnect: func(data map[string]string) bool {
			return data["token"] == "abcdefg"
		},
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	_, err = clientFn(server, func(c *Client) {
		c.AuthData = map[string]string{"token": "abcdefg"}
	})
	if err != nil {
		t.Fatal(err)
	}
}

func testRefusesUnauthedCommands(t *testing.T, clientFn func(s *testServer, conf ...func(c *Client)) (*Client, error)) {
	server, err := startServer(nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	client, err := clientFn(server, func(c *Client) {
		c.skip_auth = true
	})
	if err != nil {
		t.Fatal(err)
	}

	err = client.send("bla", nil)
	if err != nil {
		t.Fatal(err)
	}

	m, err := client.receive()
	if err != nil {
		t.Fatal(err)
	}
	if m.Type() != AuthFailedMessage && m["reason"] != "Auth expected" {
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
