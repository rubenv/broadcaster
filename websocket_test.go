package broadcaster

import "testing"

func TestWSClient(t *testing.T) {
	testClient(t, newWSClient)
}

func TestWSCanConnect(t *testing.T) {
	testCanConnect(t, newWSClient)
}

func TestWSRefusesUnauthedCommands(t *testing.T) {
	testRefusesUnauthedCommands(t, newWSClient)
}

func TestWSCanSubscribe(t *testing.T) {
	testCanSubscribe(t, newWSClient)
}
