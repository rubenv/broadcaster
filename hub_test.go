package broadcaster

import (
	"fmt"
	"testing"
	"time"
)

var testChannel = "test"

type testConnection struct {
	Messages chan string
}

func (t *testConnection) Send(channel, message string) {
	t.Messages <- fmt.Sprintf("%s - %s", channel, message)
}

func TestHubConnectDisconnect(t *testing.T) {
	hub := &hub{
		redis: newTestRedisBackend(),
	}

	err := hub.Prepare()
	if err != nil {
		t.Fatal(err)
	}

	go hub.Run()
	defer hub.Stop()

	conn := &testConnection{}

	err = hub.Connect(conn)
	if err != nil {
		t.Fatal(err)
	}

	if len(hub.subscriptions) != 1 {
		t.Errorf("Expected 1 connection, got %d", len(hub.subscriptions))
	}

	err = hub.Disconnect(conn)
	if err != nil {
		t.Fatal(err)
	}

	if len(hub.subscriptions) != 0 {
		t.Errorf("Expected 0 connections, got %d", len(hub.subscriptions))
	}
}

func TestHubSubscribe(t *testing.T) {
	hub := &hub{
		redis: newTestRedisBackend(),
	}

	err := hub.Prepare()
	if err != nil {
		t.Fatal(err)
	}

	go hub.Run()
	defer hub.Stop()

	conn := &testConnection{}

	// Should have connected first!
	err = hub.Subscribe(conn, testChannel)
	if err == nil || err.Error() != "Unknown connection" {
		t.Fatal("Expected error")
	}

	// Connect, then try again
	err = hub.Connect(conn)
	if err != nil {
		t.Fatal(err)
	}

	err = hub.Subscribe(conn, testChannel)
	if err != nil {
		t.Fatal(err)
	}
}

func TestHubUnsubscribe(t *testing.T) {
	hub := &hub{
		redis: newTestRedisBackend(),
	}

	err := hub.Prepare()
	if err != nil {
		t.Fatal(err)
	}

	go hub.Run()
	defer hub.Stop()

	conn := &testConnection{}

	// Should have connected first!
	err = hub.Unsubscribe(conn, testChannel)
	if err == nil || err.Error() != "Unknown connection" {
		t.Fatal("Expected error")
	}

	// Connect, then try again
	err = hub.Connect(conn)
	if err != nil {
		t.Fatal(err)
	}

	// Should have subscribed first!
	err = hub.Unsubscribe(conn, testChannel)
	if err == nil || err.Error() != "Not subscribed to channel test" {
		t.Fatal("Expected error")
	}

	err = hub.Subscribe(conn, testChannel)
	if err != nil {
		t.Fatal(err)
	}

	stats, err := hub.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.LocalSubscriptions[testChannel] != 1 {
		t.Errorf("Expected 1 subscription, got %d", stats.LocalSubscriptions[testChannel])
	}

	err = hub.Unsubscribe(conn, testChannel)
	if err != nil {
		t.Fatal(err)
	}

	stats, err = hub.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.LocalSubscriptions[testChannel] != 0 {
		t.Errorf("Expected 0 subscriptions, got %d", stats.LocalSubscriptions[testChannel])
	}
}

func TestHubMessage(t *testing.T) {
	hub := &hub{
		redis: newTestRedisBackend(),
	}

	err := hub.Prepare()
	if err != nil {
		t.Fatal(err)
	}

	go hub.Run()
	defer hub.Stop()

	conn := &testConnection{
		Messages: make(chan string, 10),
	}

	err = hub.Connect(conn)
	if err != nil {
		t.Fatal(err)
	}

	sendMessage(testChannel, "1")
	select {
	case <-conn.Messages:
		t.Errorf("Shouldn't have received a message!")
	default:
	}

	err = hub.Subscribe(conn, testChannel)
	if err != nil {
		t.Fatal(err)
	}

	sendMessage(testChannel, "1")
	select {
	case <-conn.Messages:
	case <-time.After(1 * time.Second):
		t.Errorf("Should have received a message!")
	}
}
