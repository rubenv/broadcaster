package broadcaster

import (
	"bytes"
	"encoding/json"
	"net/http"
)

type longpollClientTransport struct {
	client     *Client
	messages   chan clientMessage
	err        error
	id         string
	httpClient http.Client
}

func newlongpollClientTransport(c *Client) *longpollClientTransport {
	return &longpollClientTransport{
		client:     c,
		messages:   make(chan clientMessage, 10),
		httpClient: http.Client{},
	}
}

func (t *longpollClientTransport) Connect(authData map[string]string) error {
	data := authData
	if data == nil {
		data = make(map[string]string)
	}
	data["type"] = AuthMessage

	err := t.Send(data)
	if err != nil {
		return err
	}

	go t.poll()
	return nil
}

func (t *longpollClientTransport) Close() error {
	close(t.messages)
	return nil
}

func (t *longpollClientTransport) Send(data clientMessage) error {
	buf, err := json.Marshal(data)
	if err != nil {
		return err
	}

	url := t.client.url()
	resp, err := t.httpClient.Post(url, "application/json", bytes.NewBuffer(buf))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	result := []clientMessage{}
	json.NewDecoder(resp.Body).Decode(&result)
	for _, v := range result {
		t.messages <- v
	}
	return nil
}

func (t *longpollClientTransport) Receive() (clientMessage, error) {
	m, ok := <-t.messages
	if !ok {
		return nil, t.err
	}
	return m, nil
}

func (t *longpollClientTransport) poll() {
	// TODO: Keep polling for messages.
}
