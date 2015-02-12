package broadcaster

import (
	"errors"
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
var httpPort int
var httpListener *stoppableListener.StoppableListener
var httpWg sync.WaitGroup

func TestConnect(t *testing.T) {
	err := startServer(nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = newClient()
	if err != nil {
		t.Fatal(err)
	}

	stopServer()
}

func TestConnect2(t *testing.T) {
	err := startServer(nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = newClient()
	if err != nil {
		t.Fatal(err)
	}

	stopServer()
}

// Starts a redis server and uses that for the tests.
func TestMain(m *testing.M) {
	// Get random port for redis and HTTP server.
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	redisPort = 24000 + r.Intn(1000)
	httpPort = redisPort + 1

	// Start redis
	cmd := exec.Command("redis-server", "--port", strconv.Itoa(redisPort))
	err := cmd.Start()
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

	var code int = 0

	// Shut down redis when done
	defer func() {
		c, err := redis.Dial("tcp", fmt.Sprintf(":%d", redisPort))
		if err != nil {
			fmt.Println("Could not connect to redis for shutdown")
			os.Exit(1)
		}
		defer c.Close()

		c.Do("SHUTDOWN", "NOSAVE")
		cmd.Wait()

		os.Exit(code)
	}()

	// Run tests
	code = m.Run()
}

func startServer(s *Server) error {
	if httpListener != nil {
		return errors.New("Already have a HTTP server running!")
	}
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", httpPort))
	if err != nil {
		return err
	}

	httpListener, err = stoppableListener.New(listener)
	if err != nil {
		return err
	}

	if s == nil {
		s = &Server{}
	}

	mux := http.NewServeMux()

	mux.Handle("/broadcaster/", s)
	server := http.Server{Handler: mux}

	go func() {
		httpWg.Add(1)
		defer httpWg.Done()
		err := server.Serve(httpListener)
		log.Print(err)
	}()

	return nil
}

func stopServer() {
	listener := httpListener
	httpListener = nil
	httpPort += 1

	go func() {
		listener.Stop()
		httpWg.Wait()
	}()
}

func newClient() (*websocket.Conn, error) {
	url := fmt.Sprintf("ws://localhost:%d/broadcaster/", httpPort)
	log.Println(url)

	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return nil, err
	}

	return conn, nil
}
