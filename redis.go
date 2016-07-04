package broadcaster

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/garyburd/redigo/redis"
)

type redisBackend struct {
	conn           redis.Pool
	pubSub         redis.PubSubConn
	prefix         string
	timeout        int
	controlChannel string

	Messages chan redis.Message
}

const (
	redisSleep          time.Duration = 5 * time.Second
	redisPingInterval   time.Duration = 3 * time.Second
	redisConnectTimeout time.Duration = 5 * time.Second
	redisReadTimeout    time.Duration = 5 * time.Minute
	redisWriteTimeout   time.Duration = 5 * time.Second
)

func newRedisBackend(redisHost, pubsubHost, controlChannel, prefix string, timeout time.Duration) (*redisBackend, error) {
	r := newConnectionRetrier(nil)

	var p redis.Conn
	err := r.Run(func() error {
		c, err := redis.DialTimeout("tcp", pubsubHost, redisConnectTimeout, redisReadTimeout, redisWriteTimeout)
		if err != nil {
			return err
		}
		p = c
		return nil
	})
	if err != nil {
		return nil, err
	}

	pubSub := redis.PubSubConn{Conn: p}

	err = pubSub.Subscribe(controlChannel)
	if err != nil {
		pubSub.Close()
		return nil, err
	}

	b := &redisBackend{
		conn: redis.Pool{
			MaxIdle:     3,
			IdleTimeout: 60 * time.Second,
			Dial: func() (redis.Conn, error) {
				var conn redis.Conn
				err := r.Run(func() error {
					c, err := redis.DialTimeout("tcp", redisHost, redisConnectTimeout, redisReadTimeout, redisWriteTimeout)
					if err != nil {
						return err
					}
					conn = c
					return nil
				})
				return conn, err
			},
			TestOnBorrow: func(c redis.Conn, t time.Time) error {
				if time.Now().Sub(t) > redisPingInterval {
					_, err := c.Do("PING")
					return err
				}
				return nil
			},
		},
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
			b.Messages <- v
		case error:
			// Server stopped?
			return
		}
	}
}

func (b *redisBackend) key(name string, args ...interface{}) string {
	if len(args) > 0 {
		return b.prefix + fmt.Sprintf(name, args...)
	} else {
		return b.prefix + name
	}
}

func (b *redisBackend) GetConnected() (int, error) {
	conn := b.conn.Get()
	defer conn.Close()

	c, err := conn.Do("GET", b.key("connected"))
	if err != nil {
		return 0, err
	}
	if c == nil {
		return 0, nil
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

	conn := b.conn.Get()
	defer conn.Close()
	conn.Send("MULTI")
	conn.Send("SETEX", b.key("sess:"+token), b.timeout, string(data))
	conn.Send("INCR", b.key("connected"))
	_, err = conn.Do("EXEC")
	return err
}

func (b *redisBackend) DeleteSession(token string) error {
	conn := b.conn.Get()
	defer conn.Close()
	conn.Send("MULTI")
	conn.Send("DEL", b.key("sess:%s", token))
	conn.Send("DEL", b.key("channels:%s", token))
	conn.Send("DECR", b.key("connected"))
	_, err := conn.Do("EXEC")
	return err
}

func (b *redisBackend) GetSession(token string) (clientMessage, error) {
	conn := b.conn.Get()
	defer conn.Close()

	s, err := redis.Bytes(conn.Do("GET", b.key("sess:"+token)))
	if err != nil {
		return nil, err
	}

	data := clientMessage{}
	err = json.Unmarshal(s, &data)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func (b *redisBackend) IsConnected(token string) (bool, error) {
	conn := b.conn.Get()
	defer conn.Close()

	r, err := conn.Do("EXISTS", b.key("sess:"+token))
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

// Records channel subscription and broadcasts it to listeners
func (b *redisBackend) LongpollSubscribe(token, channel string) error {
	conn := b.conn.Get()
	defer conn.Close()

	key := b.key("channels:%s", token)
	conn.Send("MULTI")
	conn.Send("HSET", key, channel, "1")
	conn.Send("EXPIRE", key, b.timeout)
	conn.Send("PUBLISH", b.controlChannel, fmt.Sprintf("subscribe %s %s", token, channel))
	_, err := conn.Do("EXEC")
	if err != nil {
		return err
	}

	return nil
}

// Records channel unsubscription and broadcasts it to listeners
func (b *redisBackend) LongpollUnsubscribe(token, channel string) error {
	conn := b.conn.Get()
	defer conn.Close()

	key := b.key("channels:%s", token)
	conn.Send("MULTI")
	conn.Send("HDEL", key, channel)
	conn.Send("PUBLISH", b.controlChannel, fmt.Sprintf("unsubscribe %s %s", token, channel))
	_, err := conn.Do("EXEC")
	if err != nil {
		return err
	}

	return nil
}

func (b *redisBackend) LongpollGetChannels(token string) ([]string, error) {
	conn := b.conn.Get()
	defer conn.Close()

	key := b.key("channels:%s", token)

	c, err := redis.Strings(conn.Do("HKEYS", key))
	if err != nil {
		return nil, err
	}
	return c, err
}

func (b *redisBackend) LongpollPing(token string) error {
	conn := b.conn.Get()
	defer conn.Close()

	// Use double expire time: the initial waiting time of the request +
	// allowed lingering time.
	conn.Send("MULTI")
	conn.Send("EXPIRE", b.key("channels:%s", token), b.timeout*2)
	conn.Send("EXPIRE", b.key("sess:%s", token), b.timeout*2)
	_, err := conn.Do("EXEC")
	if err != nil {
		return err
	}

	return nil
}

func (b *redisBackend) LongpollBacklog(token string, m clientMessage) error {
	conn := b.conn.Get()
	defer conn.Close()

	// No need to store type
	delete(m, "__type")
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}

	key := b.key("backlog:%s", token)
	conn.Send("MULTI")
	conn.Send("RPUSH", key, data)
	conn.Send("EXPIRE", key, b.timeout)
	_, err = conn.Do("EXEC")
	if err != nil {
		return err
	}

	return nil
}

func (b *redisBackend) LongpollTransfer(token string, seq string) error {
	conn := b.conn.Get()
	defer conn.Close()

	_, err := conn.Do("PUBLISH", b.controlChannel, fmt.Sprintf("transfer %s %s", token, seq))
	if err != nil {
		return err
	}

	return nil
}

func (b *redisBackend) LongpollGetBacklog(token string, result chan clientMessage) {
	conn := b.conn.Get()
	defer conn.Close()

	key := b.key("backlog:%s", token)
	for {
		s, err := redis.Bytes(conn.Do("LPOP", key))
		if err != nil {
			return
		}

		data := clientMessage{}
		err = json.Unmarshal(s, &data)
		if err != nil {
			return
		}

		data["__type"] = MessageMessage

		result <- data
	}
}
