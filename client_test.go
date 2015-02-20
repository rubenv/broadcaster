package broadcaster

import (
	"testing"
	"time"
)

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
		CanConnect: func(data map[string]interface{}) bool {
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
		CanConnect: func(data map[string]interface{}) bool {
			return data["token"] == "abcdefg"
		},
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	_, err = clientFn(server, func(c *Client) {
		c.AuthData = map[string]interface{}{"token": "abcdefg"}
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

func testSubscribe(t *testing.T, clientFn func(s *testServer, conf ...func(c *Client)) (*Client, error)) {
	server, err := startServer(nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	client, err := clientFn(server)
	if err != nil {
		t.Fatal(err)
	}

	err = client.Subscribe("test")
	if err != nil {
		t.Fatal(err)
	}

	// Wait until polling socket is connected. This makes sure that the local
	// subscription count is correct.
	<-time.After(100 * time.Millisecond)

	stats, err := server.Broadcaster.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.Connections != 1 {
		t.Errorf("Unexpected connection count: %d", stats.Connections)
	}
	if stats.LocalSubscriptions["test"] != 1 {
		t.Errorf("Unexpected subscription count: %d", stats.LocalSubscriptions["test"])
	}
}

func testCanSubscribe(t *testing.T, clientFn func(s *testServer, conf ...func(c *Client)) (*Client, error)) {
	server, err := startServer(&Server{
		CanSubscribe: func(data map[string]interface{}, channel string) bool {
			return false
		},
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	client, err := clientFn(server)
	if err != nil {
		t.Fatal(err)
	}

	err = client.Subscribe("test")
	if err == nil {
		t.Fatal("Expected error!")
	}
	if err.Error() != "Subscribe error: Channel refused" {
		t.Fatal("Did not properly deny access")
	}

	stats, err := server.Broadcaster.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.LocalSubscriptions["test"] != 0 {
		t.Errorf("Unexpected subscription count: %d", stats.LocalSubscriptions["test"])
	}
}

func testMessageTypes(t *testing.T, clientFn func(s *testServer, conf ...func(c *Client)) (*Client, error)) {
	server, err := startServer(nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	client, err := clientFn(server)
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
	if m.Type() != UnknownMessage {
		t.Fatal("Did not properly refuse message type")
	}

	stats, err := server.Broadcaster.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.LocalSubscriptions["test"] != 0 {
		t.Errorf("Unexpected subscription count: %d", stats.LocalSubscriptions["test"])
	}
}

func testMessage(t *testing.T, clientFn func(s *testServer, conf ...func(c *Client)) (*Client, error)) {
	server, err := startServer(nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	client, err := clientFn(server)
	if err != nil {
		t.Fatal(err)
	}

	err = client.Subscribe("test")
	if err != nil {
		t.Fatal(err)
	}

	// Wait until polling socket is connected. There can be a small gap between
	// connecting and listening while long-polling.
	<-time.After(100 * time.Millisecond)

	err = sendMessage("test", "Test message")
	if err != nil {
		t.Fatal(err)
	}

	err = sendMessage("test", "Test message 2")
	if err != nil {
		t.Fatal(err)
	}

	m := <-client.Messages
	if m.Type() != "message" || m["channel"] != "test" || m["body"] != "Test message" {
		t.Error("Wrong message payload")
	}

	m = <-client.Messages
	if m.Type() != "message" || m["channel"] != "test" || m["body"] != "Test message 2" {
		t.Error("Wrong message payload")
	}

	// Wait until next polling interval (tests follow-up connections)
	<-time.After(100 * time.Millisecond)
	err = sendMessage("test", "Test message 3")
	if err != nil {
		t.Fatal(err)
	}

	m = <-client.Messages
	if m.Type() != "message" || m["channel"] != "test" || m["body"] != "Test message 3" {
		t.Error("Wrong message payload")
	}
}

func testMessageChannel(t *testing.T, clientFn func(s *testServer, conf ...func(c *Client)) (*Client, error)) {
	server, err := startServer(nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	client, err := clientFn(server)
	if err != nil {
		t.Fatal(err)
	}

	err = client.Subscribe("test")
	if err != nil {
		t.Fatal(err)
	}

	// Wait until polling socket is connected. There can be a small gap between
	// connecting and listening while long-polling.
	<-time.After(100 * time.Millisecond)

	err = sendMessage("other", "Test message")
	if err != nil {
		t.Fatal(err)
	}

	err = sendMessage("test", "Test message")
	if err != nil {
		t.Fatal(err)
	}

	m := <-client.Messages
	if m.Type() != "message" || m["channel"] != "test" || m["body"] != "Test message" {
		t.Error("Wrong message payload")
	}

	select {
	case <-client.Messages:
		t.Error("Unexpected message")
	default:
	}
}

func testUnsubscribe(t *testing.T, clientFn func(s *testServer, conf ...func(c *Client)) (*Client, error)) {
	server, err := startServer(nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	client, err := clientFn(server)
	if err != nil {
		t.Fatal(err)
	}

	err = client.Subscribe("test")
	if err != nil {
		t.Fatal(err)
	}

	err = client.Unsubscribe("test")
	if err != nil {
		t.Fatal(err)
	}

	// Wait for client to catch up
	<-time.After(100 * time.Millisecond)

	stats, err := server.Broadcaster.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.Connections != 1 {
		t.Errorf("Unexpected connection count: %d", stats.Connections)
	}
	if stats.LocalSubscriptions["test"] != 0 {
		t.Errorf("Unexpected subscription count: %d", stats.LocalSubscriptions["test"])
	}
}
