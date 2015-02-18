package broadcaster

import "testing"

func TestWSConnect(t *testing.T) {
	testConnect(t, newWSClient)
}

func TestWSCanConnect(t *testing.T) {
	testCanConnect(t, newWSClient)
}

func TestWSAuthData(t *testing.T) {
	testAuthData(t, newWSClient)
}

func TestWSRefusesUnauthedCommands(t *testing.T) {
	testRefusesUnauthedCommands(t, newWSClient)
}

func TestWSSubscribe(t *testing.T) {
	testSubscribe(t, newWSClient)
}

func TestWSCanSubscribe(t *testing.T) {
	testCanSubscribe(t, newWSClient)
}

func TestWSMessageTypes(t *testing.T) {
	testMessageTypes(t, newWSClient)
}

func TestWSMessage(t *testing.T) {
	testMessage(t, newWSClient)
}

func TestWSUnsubscribe(t *testing.T) {
	testUnsubscribe(t, newWSClient)
}
