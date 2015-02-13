package broadcaster

import (
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/garyburd/redigo/redis"
	"github.com/gorilla/websocket"
	"github.com/hydrogen18/stoppableListener"
)

var redisPort int
var redisClient redis.Conn
var portSource = rand.New(rand.NewSource(26))

// Starts a redis server and uses that for the tests.
func TestMain(m *testing.M) {
	// Get random port for redis
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	redisPort = 24000 + r.Intn(1000)

	// Log files
	serverOut, err := os.Create("/tmp/broadcaster-redis-server.log")
	if err != nil {
		fmt.Printf("Could not open server log: %s", err.Error())
		return
	}
	monitorOut, err := os.Create("/tmp/broadcaster-redis.log")
	if err != nil {
		fmt.Printf("Could not open monitor log: %s", err.Error())
		return
	}

	// Start redis
	cmd := exec.Command("redis-server", "--port", strconv.Itoa(redisPort))
	cmd.Stdout = serverOut
	err = cmd.Start()
	if err != nil {
		fmt.Printf("Could not start redis on port %d\n", redisPort)
		os.Exit(1)
	}

	// Hammer it until it runs
	awake := false
	for !awake {
		c, err := redis.Dial("tcp", fmt.Sprintf(":%d", redisPort))
		if err == nil {
			c.Close()
			awake = true
		}
	}

	// Redis client
	redisClient, err = redis.Dial("tcp", fmt.Sprintf(":%d", redisPort))
	if err != nil {
		fmt.Println("Could not connect to redis")
		os.Exit(1)
	}

	// Monitor the redis server to make debugging easier
	monitorCmd := exec.Command("redis-cli", "-p", strconv.Itoa(redisPort), "monitor")
	monitorCmd.Stdout = monitorOut
	err = monitorCmd.Start()
	if err != nil {
		fmt.Printf("Could not start redis monitor\n")
		os.Exit(1)
	}

	var code int

	// Shut down redis when done
	defer func() {
		defer redisClient.Close()
		defer serverOut.Close()
		defer monitorOut.Close()

		redisClient.Do("SHUTDOWN", "NOSAVE")
		cmd.Wait()

		os.Exit(code)
	}()

	// Run tests
	code = m.Run()
}

type testServer struct {
	Port int

	Listener    *stoppableListener.StoppableListener
	Broadcaster *Server
	HTTPServer  http.Server
	wg          sync.WaitGroup
}

func startServer(s *Server, port int) (*testServer, error) {
	if port == 0 {
		// Fixed seed to reproducably get random ports
		port = 25000 + portSource.Intn(1000)
	}
	server := &testServer{
		Port:        port,
		Broadcaster: s,
	}
	err := server.Start()
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

	httpListener, err := stoppableListener.New(listener)
	if err != nil {
		return err
	}

	s.Listener = httpListener

	if s.Broadcaster == nil {
		s.Broadcaster = &Server{}
	}

	mux := http.NewServeMux()

	mux.Handle("/broadcaster/", s.Broadcaster)
	s.HTTPServer = http.Server{Handler: mux}

	go func() {
		s.wg.Add(1)
		defer s.wg.Done()
		err := s.HTTPServer.Serve(s.Listener)
		log.Print(err)
	}()

	return nil
}

func (s *testServer) Stop() {
	go func() {
		s.Listener.Stop()
		s.wg.Wait()
	}()
}

type clientError struct {
	Response   *http.Response
	ProtoError error
}

func (e clientError) Error() string {
	return e.ProtoError.Error()
}

func newClient(s *testServer) (*websocket.Conn, error) {
	url := fmt.Sprintf("ws://localhost:%d/broadcaster/", s.Port)

	conn, resp, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return nil, clientError{
			Response:   resp,
			ProtoError: err,
		}
	}

	return conn, nil
}

func sendMessage(channel, message string) error {
	_, err := redisClient.Do("PUBLISH", channel, message)
	return err
}
