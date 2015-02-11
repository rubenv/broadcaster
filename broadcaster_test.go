package broadcaster

import (
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/garyburd/redigo/redis"
)

var port int

func TestConnect(t *testing.T) {
	t.Log("Inside test")
}

// Starts a redis server and uses that for the tests.
func TestMain(m *testing.M) {
	// Get random port for redis.
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	port = 24000 + r.Intn(1000)

	// Start redis
	cmd := exec.Command("redis-server", "--port", strconv.Itoa(port))
	err := cmd.Start()
	if err != nil {
		fmt.Printf("Could not start redis on port %d\n", port)
		os.Exit(1)
	}

	// Hammer it until it runs
	awake := false
	for !awake {
		c, err := redis.Dial("tcp", fmt.Sprintf(":%d", port))
		if err == nil {
			c.Close()
			awake = true
		}
	}

	var code int = 0

	// Shut down redis when done
	defer func() {
		c, err := redis.Dial("tcp", fmt.Sprintf(":%d", port))
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
