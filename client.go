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
	if err != nil || auth.Type != "auth" {
		c.Close(401, "Auth expected")
		return
	}

	if c.Server.CanConnect != nil && !c.Server.CanConnect(auth.Data) {
		c.Close(401, "Unauthorized")
		return
	}

	conn.WriteJSON(clientMessage{
		Type: "authOk",
	})

	c.Server.hub.NewClient <- c
}

func (c *websocketClient) Close(code uint16, msg string) {
	payload := make([]byte, 2)
	binary.BigEndian.PutUint16(payload, code)
	payload = append(payload, []byte(msg)...)
	c.Conn.WriteMessage(websocket.CloseMessage, payload)
	c.Conn.Close()
}
