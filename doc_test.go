package broadcaster

import (
	"log"
	"net/http"
)

// Pass options to Server to configure Redis etc.
func Example() {
	s := &Server{
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
	s := &Server{}

	// Always call Prepare() first!
	err := s.Prepare()
	if err != nil {
		panic(err)
	}

	http.Handle("/broadcaster/", s)

	log.Fatal(http.ListenAndServe(":8080", nil))
}
