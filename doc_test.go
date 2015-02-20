package broadcaster

import (
	"log"
	"net/http"

	"github.com/rubenv/broadcaster"
)

// Pass options to Server to configure Redis etc.
func Example() {
	s := &broadcaster.Server{
		RedisHost: "myredis.local:6379",
	}

	// Always call Prepare() first!
	err := s.Prepare()
	if err != nil {
		panic(err)
	}

	http.Handle("/broadcaster/", s)

	log.Fatal(http.ListenAndServe(":8080", nil))
}

// Starting a new server
func ExampleServer_ServeHTTP() {
	s := &broadcaster.Server{}

	// Always call Prepare() first!
	err := s.Prepare()
	if err != nil {
		panic(err)
	}

	http.Handle("/broadcaster/", s)

	log.Fatal(http.ListenAndServe(":8080", nil))
}
