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

	// Client: I'm still alive
	PingMessage = "ping"

	// Server: Unknown message
	UnknownMessage = "unknown"

	// Server: Server error
	ServerErrorMessage = "serverError"
)

type ClientMessage map[string]interface{}

func (c ClientMessage) ResultId() string {
	t := c.Type()
	if t == SubscribeOKMessage || t == SubscribeErrorMessage {
		t = SubscribeMessage
	}
	if t == UnsubscribeOKMessage {
		t = UnsubscribeMessage
	}
	return fmt.Sprintf("%s_%s", t, c["channel"])
}

func (c ClientMessage) Type() string {
	s, ok := c["__type"].(string)
	if !ok {
		return ""
	}
	return s
}

func (c ClientMessage) Token() string {
	s, ok := c["__token"].(string)
	if !ok {
		return ""
	}
	return s
}

func (c ClientMessage) Channel() string {
	s, ok := c["channel"].(string)
	if !ok {
		return ""
	}
	return s
}

func (c ClientMessage) Reason() string {
	s, ok := c["reason"].(string)
	if !ok {
		return ""
	}
	return s
}

func newMessage(t string) ClientMessage {
	return ClientMessage{
		"__type": t,
	}
}

func newErrorMessage(t string, err error) ClientMessage {
	return ClientMessage{
		"__type": t,
		"reason": err.Error(),
	}
}

func newChannelMessage(t, channel string) ClientMessage {
	return ClientMessage{
		"__type":  t,
		"channel": channel,
	}
}

func newBroadcastMessage(channel, body string) ClientMessage {
	return ClientMessage{
		"__type":  MessageMessage,
		"channel": channel,
		"body":    body,
	}
}

func newChannelErrorMessage(t, channel string, err error) ClientMessage {
	return ClientMessage{
		"__type":  t,
		"channel": channel,
		"reason":  err.Error(),
	}
}
