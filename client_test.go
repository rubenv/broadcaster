package broadcaster

import (
	"testing"
	"time"
)

import _ "net/http/pprof"

func init() {
	//runtime.GOMAXPROCS(runtime.NumCPU())
	//go func() {
	//		log.Println(http.ListenAndServe("localhost:6060", nil))
	//	}()
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

	client, err := clientFn(server)
	if err == nil || err.Error() != "Auth error: Unauthorized" {
		t.Fatal("Did not properly deny access")
	}
	if client != nil {
		t.Fatal("Did not expect client")
	}

	stats, err := server.Broadcaster.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.Connections != 0 {
		t.Errorf("Unexpected connection count: %d", stats.Connections)
	}
}

func testClient(t *testing.T, clientFn func(s *testServer, conf ...func(c *Client)) (*Client, error)) {
	server, err := startServer(&Server{
		CanConnect: func(data map[string]interface{}) bool {
			return data["token"] == "abcdefg"
		},
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	// Connect + authenticate
	client, err := clientFn(server, func(c *Client) {
		c.AuthData = map[string]interface{}{"token": "abcdefg"}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Disconnect()

	stats, err := server.Broadcaster.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.Connections != 1 {
		t.Errorf("Unexpected connection count: %d", stats.Connections)
	}

	// Subscribe
	err = client.Subscribe("test")
	if err != nil {
		t.Fatal(err)
	}

	ready := false
	for !ready {
		stats, _ := server.Broadcaster.Stats()
		if stats.LocalSubscriptions["test"] != 1 {
			// Wait until polling socket is connected. There can be a small gap between
			// connecting and listening while long-polling.
			<-time.After(100 * time.Millisecond)
		} else {
			ready = true
		}
	}

	stats, err = server.Broadcaster.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.Connections != 1 {
		t.Errorf("Unexpected connection count: %d", stats.Connections)
	}
	if stats.LocalSubscriptions["test"] != 1 {
		t.Errorf("Unexpected subscription count: %d", stats.LocalSubscriptions["test"])
	}

	// Shouldn't get message on other channel
	err = server.sendMessage("other", "Other message")
	if err != nil {
		t.Fatal(err)
	}

	// Receive messages
	err = server.sendMessage("test", "Test message")
	if err != nil {
		t.Fatal(err)
	}

	err = server.sendMessage("test", "Test message 2")
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
	err = server.sendMessage("test", "Test message 3")
	if err != nil {
		t.Fatal(err)
	}

	m = <-client.Messages
	if m.Type() != "message" || m["channel"] != "test" || m["body"] != "Test message 3" {
		t.Error("Wrong message payload")
	}

	// Unsubscribe
	err = client.Unsubscribe("test")
	if err != nil {
		t.Fatal(err)
	}

	// Wait for client to catch up
	ready = false
	for !ready {
		stats, _ := server.Broadcaster.Stats()
		if stats.LocalSubscriptions["test"] != 0 {
			<-time.After(100 * time.Millisecond)
		} else {
			ready = true
		}
	}

	stats, err = server.Broadcaster.Stats()
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
	defer client.Disconnect()

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
	defer client.Disconnect()

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
