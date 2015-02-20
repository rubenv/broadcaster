package broadcaster

import "fmt"

// Message types used between server and client.
const (
	// Client: start authentication
	AuthMessage = "auth"

	// Server: Authentication succeeded
	AuthOKMessage = "authOk"

	// Server: Authentication failed
	AuthFailedMessage = "authError"

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

type clientMessage map[string]interface{}

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
	s, ok := c["__type"].(string)
	if !ok {
		return ""
	}
	return s
}

func (c clientMessage) Token() string {
	s, ok := c["__token"].(string)
	if !ok {
		return ""
	}
	return s
}

func (c clientMessage) Channel() string {
	s, ok := c["channel"].(string)
	if !ok {
		return ""
	}
	return s
}

func newMessage(t string) clientMessage {
	return clientMessage{
		"__type": t,
	}
}

func newErrorMessage(t string, err error) clientMessage {
	return clientMessage{
		"__type": t,
		"reason": err.Error(),
	}
}

func newChannelMessage(t, channel string) clientMessage {
	return clientMessage{
		"__type":  t,
		"channel": channel,
	}
}

func newBroadcastMessage(channel, body string) clientMessage {
	return clientMessage{
		"__type":  MessageMessage,
		"channel": channel,
		"body":    body,
	}
}

func newChannelErrorMessage(t, channel string, err error) clientMessage {
	return clientMessage{
		"__type":  t,
		"channel": channel,
		"reason":  err.Error(),
	}
}
