package broadcaster

import (
	"encoding/json"
	"time"

	"github.com/garyburd/redigo/redis"
)

type redisBackend struct {
	conn           redis.Conn
	pubSub         redis.PubSubConn
	prefix         string
	timeout        time.Duration
	controlChannel string

	Messages chan redis.Message
}

func newRedisBackend(redisHost, pubsubHost, controlChannel, prefix string, timeout time.Duration) (*redisBackend, error) {
	r, err := redis.Dial("tcp", redisHost)
	if err != nil {
		return nil, err
	}

	p, err := redis.Dial("tcp", pubsubHost)
	if err != nil {
		r.Close()
		return nil, err
	}
	pubSub := redis.PubSubConn{Conn: p}

	err = pubSub.Subscribe(controlChannel)
	if err != nil {
		r.Close()
		pubSub.Close()
		return nil, err
	}

	b := &redisBackend{
		conn:           r,
		pubSub:         pubSub,
		prefix:         prefix,
		timeout:        timeout,
		controlChannel: controlChannel,
		Messages:       make(chan redis.Message, 250),
	}

	go b.listen()

	return b, nil
}

func (b *redisBackend) listen() {
	for {
		switch v := b.pubSub.Receive().(type) {
		case redis.Message:
			if v.Channel == b.controlChannel {
				//h.handleControlMessage(v)
			} else {
				b.Messages <- v
			}
		case error:
			// Server stopped?
			return
		}
	}
}

func (b *redisBackend) key(name string) string {
	return b.prefix + name
}

func (b *redisBackend) StoreSession(token string, auth clientMessage) error {
	// No need to store these
	delete(auth, "__token")
	delete(auth, "__type")
	data, err := json.Marshal(auth)
	if err != nil {
		return err
	}
	_, err = b.conn.Do("SETEX", b.key("sess:"+token), b.timeout, string(data))
	return err
}

func (b *redisBackend) GetSession(token string) (clientMessage, error) {
	// TODO
	return nil, nil
}

func (b *redisBackend) Subscribe(channel string) error {
	return b.pubSub.Subscribe(channel)
}

func (b *redisBackend) Unsubscribe(channel string) error {
	return b.pubSub.Unsubscribe(channel)
}
