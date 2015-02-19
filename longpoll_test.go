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
func TestLPSubscribe(t *testing.T) {
	testSubscribe(t, newLPClient)
}

func TestLPCanSubscribe(t *testing.T) {
	testCanSubscribe(t, newLPClient)
}

func TestLPMessageTypes(t *testing.T) {
	testMessageTypes(t, newLPClient)
}
*/

/*
func TestLPMessage(t *testing.T) {
	testMessage(t, newLPClient)
}
*/

/*func TestLPUnsubscribe(t *testing.T) {
	testUnsubscribe(t, newLPClient)
}*/

// TODO: Test switching between servers, known tokens from other server should be accepted and transferred.
