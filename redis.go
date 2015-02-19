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
	timeout        int
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
		timeout:        int(timeout.Seconds()) + 1,
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

func (b *redisBackend) GetConnected() (int, error) {
	c, err := b.conn.Do("GET", b.key("connected"))
	if err != nil {
		return 0, err
	}

	r, err := redis.Int(c, err)
	if err != nil && err != redis.ErrNil {
		return 0, err
	}
	return r, nil
}

func (b *redisBackend) StoreSession(token string, auth clientMessage) error {
	// No need to store these
	delete(auth, "__token")
	delete(auth, "__type")
	data, err := json.Marshal(auth)
	if err != nil {
		return err
	}
	b.conn.Send("MULTI")
	b.conn.Send("SETEX", b.key("sess:"+token), b.timeout, string(data))
	b.conn.Send("INCR", b.key("connected"))
	_, err = b.conn.Do("EXEC")
	return err
}

func (b *redisBackend) DeleteSession(token string) error {
	b.conn.Send("MULTI")
	b.conn.Send("DEL", b.key("sess:"+token))
	b.conn.Send("DECR", b.key("connected"))
	_, err := b.conn.Do("EXEC")
	return err
}

func (b *redisBackend) GetSession(token string) (clientMessage, error) {
	// TODO
	return nil, nil
}

func (b *redisBackend) IsConnected(token string) (bool, error) {
	r, err := b.conn.Do("EXISTS", b.key("sess:"+token))
	if err != nil {
		return false, err
	}
	return r.(int64) == 1, nil
}

func (b *redisBackend) Subscribe(channel string) error {
	return b.pubSub.Subscribe(channel)
}

func (b *redisBackend) Unsubscribe(channel string) error {
	return b.pubSub.Unsubscribe(channel)
}
