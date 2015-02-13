# broadcaster

[![Build Status](https://travis-ci.org/rubenv/broadcaster.svg?branch=master)](https://travis-ci.org/rubenv/broadcaster) [![GoDoc](https://godoc.org/github.com/rubenv/broadcaster?status.png)](https://godoc.org/github.com/rubenv/broadcaster)

Package broadcaster implements a websocket server for broadcasting Redis pub/sub
messages to web clients.

# Work In Progress!

Originally based on https://github.com/rubenv/node-broadcast-hub but
significantly improved while moving to Go.

## Installation
```
go get github.com/rubenv/broadcaster
```

Import into your application with:

```go
import "github.com/rubenv/broadcaster"
```

## Usage

#### type Server

```go
type Server struct {
	// Invoked upon initial connection, can be used to enforce access control.
	CanConnect func(data map[string]string) bool

	// Invoked upon channel subscription, can be used to enforce access control
	// for channels.
	CanSubscribe func(data map[string]string, channel string) bool

	// Can be used to configure buffer sizes etc.
	// See http://godoc.org/github.com/gorilla/websocket#Upgrader
	Upgrader websocket.Upgrader
}
```

A Server is the main class of this package, pass it to http.Handle on a chosen
path to start a broadcast server.

#### func (*Server) Prepare

```go
func (s *Server) Prepare() error
```

#### func (*Server) ServeHTTP

```go
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request)
```
Main HTTP server.

#### func (*Server) Stats

```go
func (s *Server) Stats() (Stats, error)
```

#### type Stats

```go
type Stats struct {
	// Number of active connections
	Connections int
}
```

Server statistics

## License

    (The MIT License)

    Copyright (C) 2013-2015 by Ruben Vermeersch <ruben@rocketeer.be>

    Permission is hereby granted, free of charge, to any person obtaining a copy
    of this software and associated documentation files (the "Software"), to deal
    in the Software without restriction, including without limitation the rights
    to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
    copies of the Software, and to permit persons to whom the Software is
    furnished to do so, subject to the following conditions:

    The above copyright notice and this permission notice shall be included in
    all copies or substantial portions of the Software.

    THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
    IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
    FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
    AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
    LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
    OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
    THE SOFTWARE.
