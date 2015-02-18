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
