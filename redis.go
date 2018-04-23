package broadcaster

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"github.com/eapache/go-resiliency/retrier"
	"github.com/gomodule/redigo/redis"
)

type redisBackend struct {
	conn           redis.Pool
	pubSub         redis.PubSubConn
	pubSubHost     string
	prefix         string
	timeout        int
	controlChannel string
	listening      bool
	listeningLock  sync.Mutex
	controlWait    sync.WaitGroup

	dialRetrier *retrier.Retrier
	dialOptions []redis.DialOption

	subscriptions     map[string]bool
	subscriptionsLock sync.Mutex

	Messages chan redis.Message
}

const (
	redisSleep          time.Duration = 1 * time.Second
	redisPingInterval   time.Duration = 3 * time.Second
	redisConnectTimeout time.Duration = 5 * time.Second
	redisReadTimeout    time.Duration = 5 * time.Minute
	redisWriteTimeout   time.Duration = 5 * time.Second
)

func newRedisBackend(redisHost, pubSubHost, controlChannel, prefix string, timeout time.Duration) (*redisBackend, error) {
	r := newConnectionRetrier(nil)

	opts := []redis.DialOption{
		redis.DialConnectTimeout(redisConnectTimeout),
		redis.DialReadTimeout(redisReadTimeout),
		redis.DialWriteTimeout(redisWriteTimeout),
	}

	b := &redisBackend{
		conn: redis.Pool{
			MaxIdle:     3,
			IdleTimeout: 60 * time.Second,
			Dial: func() (redis.Conn, error) {
				var conn redis.Conn
				err := r.Run(func() error {
					c, err := redis.Dial("tcp", redisHost, opts...)
					if err != nil {
						return err
					}
					conn = c
					return nil
				})
				return conn, err
			},
			TestOnBorrow: func(c redis.Conn, t time.Time) error {
				if time.Since(t) > redisPingInterval {
					_, err := c.Do("PING")
					return err
				}
				return nil
			},
		},
		dialOptions:    opts,
		dialRetrier:    r,
		prefix:         prefix,
		pubSubHost:     pubSubHost,
		timeout:        int(timeout.Seconds()) + 1,
		controlChannel: controlChannel,
		subscriptions:  make(map[string]bool),
		Messages:       make(chan redis.Message, 250),
	}
	b.controlWait.Add(1)

	go b.listen()

	return b, nil
}

func (b *redisBackend) listen() {
	for {
		err := b.receive()
		if err != nil && err != io.EOF {
			log.Printf("Redis error: %s", err)
		}
		b.controlWait.Add(1)

		// Sleep until next iteration
		time.Sleep(redisSleep)
	}
}

func (b *redisBackend) connect() error {
	b.listeningLock.Lock()
	b.listening = false
	b.listeningLock.Unlock()

	var p redis.Conn
	err := b.dialRetrier.Run(func() error {
		c, err := redis.Dial("tcp", b.pubSubHost, b.dialOptions...)
		if err != nil {
			return err
		}
		p = c
		return nil
	})
	if err != nil {
		return err
	}

	b.pubSub = redis.PubSubConn{Conn: p}

	err = b.pubSub.Subscribe(b.controlChannel)
	if err != nil {
		b.pubSub.Close()
		return err
	}

	b.subscriptionsLock.Lock()
	defer b.subscriptionsLock.Unlock()
	for k, _ := range b.subscriptions {
		err = b.pubSub.Subscribe(k)
		if err != nil {
			b.pubSub.Close()
			return err
		}
	}

	b.listeningLock.Lock()
	b.listening = true
	b.listeningLock.Unlock()
	b.controlWait.Done()
	return nil
}

func (b *redisBackend) receive() error {
	err := b.connect()
	if err != nil {
		return err
	}

	for {
		switch v := b.pubSub.Receive().(type) {
		case redis.Message:
			b.Messages <- v
		case error:
			// Server stopped?
			return v.(error)
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

func (b *redisBackend) StoreSession(token string, auth ClientMessage) error {
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

func (b *redisBackend) GetSession(token string) (ClientMessage, error) {
	conn := b.conn.Get()
	defer conn.Close()

	s, err := redis.Bytes(conn.Do("GET", b.key("sess:"+token)))
	if err != nil {
		return nil, err
	}

	data := ClientMessage{}
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
	b.controlWait.Wait()
	b.subscriptionsLock.Lock()
	defer b.subscriptionsLock.Unlock()
	b.subscriptions[channel] = true
	return b.pubSub.Subscribe(channel)
}

func (b *redisBackend) Unsubscribe(channel string) error {
	b.controlWait.Wait()
	b.subscriptionsLock.Lock()
	defer b.subscriptionsLock.Unlock()
	delete(b.subscriptions, channel)
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
	return err
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
	return err
}

func (b *redisBackend) LongpollGetChannels(token string) ([]string, error) {
	conn := b.conn.Get()
	defer conn.Close()

	key := b.key("channels:%s", token)

	return redis.Strings(conn.Do("HKEYS", key))
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
	return err
}

func (b *redisBackend) LongpollBacklog(token string, m ClientMessage) error {
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
	return err
}

func (b *redisBackend) LongpollTransfer(token string, seq string) error {
	conn := b.conn.Get()
	defer conn.Close()

	_, err := conn.Do("PUBLISH", b.controlChannel, fmt.Sprintf("transfer %s %s", token, seq))
	return err
}

func (b *redisBackend) LongpollGetBacklog(token string, result chan ClientMessage) {
	conn := b.conn.Get()
	defer conn.Close()

	key := b.key("backlog:%s", token)
	for {
		s, err := redis.Bytes(conn.Do("LPOP", key))
		if err != nil {
			return
		}

		data := ClientMessage{}
		err = json.Unmarshal(s, &data)
		if err != nil {
			return
		}

		data["__type"] = MessageMessage

		result <- data
	}
}

func (b *redisBackend) IsListening() bool {
	b.listeningLock.Lock()
	defer b.listeningLock.Unlock()
	return b.listening
}
