package broadcaster

import "fmt"

// Message types used between server and client.
const (
	// Client: start authentication
	AuthMessage = "auth"

	// Server: Authentication succeeded
	AuthOKMessage = "authOk"

	// Server: Authentication failed
	AuthFailedMessage = "authFailed"

	// Client: Subscribe to channel
	SubscribeMessage = "subscribe"

	// Server: Subscribe succeeded
	SubscribeOKMessage = "subscribeOk"

	// Server: Subscribe failed
	SubscribeErrorMessage = "subscribeError"

	// Server: Broadcast message
	MessageMessage = "message"

	// Client: Unsubscribe from channel
	UnsubscribeMessage = "unsubscribe"

	// Server: Unsubscribe succeeded
	UnsubscribeOKMessage = "unsubscribeOk"

	// Server: Unsubscribe failed
	UnsubscribeErrorMessage = "unsubscribeError"

	// Client: Send me more messages
	PollMessage = "poll"

	// Server: Unknown message
	UnknownMessage = "unknown"

	// Server: Server error
	ServerErrorMessage = "serverError"
)

type clientMessage map[string]string

func (c clientMessage) ResultId() string {
	t := c.Type()
	if t == SubscribeOKMessage || t == SubscribeErrorMessage {
		t = SubscribeMessage
	}
	if t == UnsubscribeOKMessage {
		t = UnsubscribeMessage
	}
	return fmt.Sprintf("%s_%s", t, c["channel"])
}

func (c clientMessage) Type() string {
	return c["__type"]
}

func (c clientMessage) Token() string {
	return c["__token"]
}
