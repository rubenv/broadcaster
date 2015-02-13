package broadcaster

import (
	"encoding/binary"
	"net/http"

	"github.com/gorilla/websocket"
)

type client interface{}

// Websocket clients
type websocketClient struct {
	Conn   *websocket.Conn
	Server *Server
}

func newWebsocketClient(w http.ResponseWriter, r *http.Request, s *Server) {
	client := &websocketClient{
		Server: s,
	}
	client.handshake(w, r)
}

func (c *websocketClient) handshake(w http.ResponseWriter, r *http.Request) {
	conn, err := c.Server.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	c.Conn = conn

	// Expect auth packet first.
	auth := clientMessage{}
	err = conn.ReadJSON(&auth)
	if err != nil || auth["type"] != "auth" {
		c.Close(401, "Auth expected")
		return
	}

	if c.Server.CanConnect != nil && !c.Server.CanConnect(auth) {
		c.Close(401, "Unauthorized")
		return
	}

	conn.WriteJSON(clientMessage{"type": "authOk"})

	hub := c.Server.hub

	hub.NewClient <- c

	defer func() {
		hub.ClientDisconnect <- c
		conn.Close()
	}()
	m := clientMessage{}
	for {
		err := conn.ReadJSON(&m)
		if err != nil {
			c.Close(400, err.Error())
			break
		}

		switch m["type"] {
		case "subscribe":
			channel := m["channel"]
			if c.Server.CanSubscribe != nil && !c.Server.CanSubscribe(auth, channel) {
				c.Close(403, "Channel refused")
				continue
			}

			hub.Subscribe <- subscription{
				Client:  c,
				Channel: channel,
			}

			conn.WriteJSON(clientMessage{
				"type":    "subscribeOk",
				"channel": channel,
			})
		default:
			c.Close(400, "Unexpected message")
			break
		}
	}
}

func (c *websocketClient) Close(code uint16, msg string) {
	payload := make([]byte, 2)
	binary.BigEndian.PutUint16(payload, code)
	payload = append(payload, []byte(msg)...)
	c.Conn.WriteMessage(websocket.CloseMessage, payload)
	c.Conn.Close()
}
