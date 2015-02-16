package broadcaster

type client interface {
	Send(channel, message string)
}
