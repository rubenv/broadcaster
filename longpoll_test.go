package broadcaster

import "testing"

func TestLPClient(t *testing.T) {
	testClient(t, newLPClient)
}

func TestLPCanConnect(t *testing.T) {
	testCanConnect(t, newLPClient)
}

/*
func TestLPRefusesUnauthedCommands(t *testing.T) {
	testRefusesUnauthedCommands(t, newLPClient)
}
*/

func TestLPCanSubscribe(t *testing.T) {
	testCanSubscribe(t, newLPClient)
}

// TODO: Test switching between servers, known tokens from other server should be accepted and transferred.
// TODO: Keep listening after longpoll disconnect, until transferred to different request.
