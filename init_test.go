package broadcaster

import (
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/garyburd/redigo/redis"
)

type testRedis struct {
	Port       int
	Client     redis.Conn
	serverOut  *os.File
	serverCmd  *exec.Cmd
	monitorOut *os.File
	monitorCmd *exec.Cmd
}

var portSource = rand.New(rand.NewSource(26))

func startRedis() (*testRedis, error) {
	s := &testRedis{}

	// Get random port for redis
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	s.Port = 24000 + r.Intn(1000)

	// Log files
	serverOut, err := os.Create("/tmp/broadcaster-redis-server.log")
	if err != nil {
		fmt.Printf("Could not open server log: %s", err.Error())
		return nil, err
	}
	s.serverOut = serverOut
	monitorOut, err := os.Create("/tmp/broadcaster-redis.log")
	if err != nil {
		fmt.Printf("Could not open monitor log: %s", err.Error())
		return nil, err
	}
	s.monitorOut = monitorOut

	// Start redis
	cmd := exec.Command("redis-server", "--port", strconv.Itoa(s.Port), "--loglevel", "debug")
	cmd.Stdout = serverOut
	cmd.Stderr = serverOut
	err = cmd.Start()
	if err != nil {
		fmt.Printf("Could not start redis on port %d\n", s.Port)
		return nil, err
	}
	s.serverCmd = cmd

	// Hammer it until it runs
	awake := false
	for !awake {
		c, err := redis.Dial("tcp", fmt.Sprintf(":%d", s.Port))
		if err == nil {
			c.Close()
			awake = true
		}
	}

	// Redis client
	redisClient, err := redis.Dial("tcp", fmt.Sprintf(":%d", s.Port))
	if err != nil {
		fmt.Println("Could not connect to redis")
		os.Exit(1)
	}
	s.Client = redisClient

	// Monitor the redis server to make debugging easier
	monitorCmd := exec.Command("redis-cli", "-p", strconv.Itoa(s.Port), "monitor")
	monitorCmd.Stdout = monitorOut
	monitorCmd.Stderr = monitorOut
	err = monitorCmd.Start()
	if err != nil {
		fmt.Printf("Could not start redis monitor\n")
		os.Exit(1)
	}
	s.monitorCmd = monitorCmd

	return s, nil
}

func (t *testRedis) sendMessage(channel, message string) error {
	_, err := t.Client.Do("PUBLISH", channel, message)
	return err
}

func (t *testRedis) Stop() {
	t.monitorCmd.Process.Kill()

	t.Client.Do("SHUTDOWN", "NOSAVE")

	t.Client.Close()
	t.serverOut.Close()
	t.monitorOut.Close()

	t.serverCmd.Wait()
}

type testServer struct {
	Port int

	Broadcaster *Server
	HTTPServer  http.Server

	Redis *testRedis
}

func startServer(s *Server, port int) (*testServer, error) {
	r, err := startRedis()
	if err != nil {
		return nil, err
	}

	if port == 0 {
		// Fixed seed to reproducably get random ports
		port = 25000 + portSource.Intn(1000)
	}
	server := &testServer{
		Port:        port,
		Broadcaster: s,
		Redis:       r,
	}
	err = server.Start()
	if err != nil {
		return nil, err
	}
	return server, nil
}

func (s *testServer) Start() error {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", s.Port))
	if err != nil {
		return err
	}

	if s.Broadcaster == nil {
		s.Broadcaster = &Server{}
	}

	s.Broadcaster.RedisHost = fmt.Sprintf("localhost:%d", s.Redis.Port)
	s.Broadcaster.Timeout = 1 * time.Second
	s.Broadcaster.PollTime = 100 * time.Millisecond

	err = s.Broadcaster.Prepare()
	if err != nil {
		return err
	}

	mux := http.NewServeMux()

	mux.Handle("/broadcaster/", s.Broadcaster)
	s.HTTPServer = http.Server{Handler: mux}

	go func() {
		s.HTTPServer.Serve(listener)
	}()

	return nil
}

func (s *testServer) Stop() {
	s.Redis.Stop()
}

func (s *testServer) sendMessage(channel, message string) error {
	return s.Redis.sendMessage(channel, message)
}

func newTestRedisBackend() (*redisBackend, *testRedis) {
	s, err := startRedis()
	if err != nil {
		panic(err)
	}
	u := fmt.Sprintf("localhost:%d", s.Port)
	b, err := newRedisBackend(u, u, "broadcaster", "bc:", 1*time.Second)
	if err != nil {
		panic(err)
	}
	return b, s
}

func newWSClient(s *testServer, conf ...func(c *Client)) (*Client, error) {
	url := fmt.Sprintf("http://localhost:%d/broadcaster/", s.Port)
	client, err := NewClient(url)
	if err != nil {
		return nil, err
	}
	client.Mode = ClientModeWebsocket

	for _, v := range conf {
		v(client)
	}

	err = client.Connect()
	if err != nil {
		return nil, err
	}

	return client, nil
}

func newLPClient(s *testServer, conf ...func(c *Client)) (*Client, error) {
	url := fmt.Sprintf("http://localhost:%d/broadcaster/", s.Port)
	client, err := NewClient(url)
	if err != nil {
		return nil, err
	}
	client.Mode = ClientModeLongPoll

	for _, v := range conf {
		v(client)
	}

	err = client.Connect()
	if err != nil {
		return nil, err
	}

	return client, nil
}
