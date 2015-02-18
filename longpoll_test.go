package broadcaster

import "testing"

func TestLPConnect(t *testing.T) {
	testConnect(t, newLPClient)
}

func TestLPCanConnect(t *testing.T) {
	testCanConnect(t, newLPClient)
}

func TestLPAuthData(t *testing.T) {
	testAuthData(t, newLPClient)
}

func TestLPRefusesUnauthedCommands(t *testing.T) {
	testRefusesUnauthedCommands(t, newLPClient)
}

/*
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
*/
