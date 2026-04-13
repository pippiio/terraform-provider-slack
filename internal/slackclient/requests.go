package slackclient

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type Response struct {
	Ts       string        `json:"ts,omitempty"`
	Channel  string        `json:"channel,omitempty"`
	Err      string        `json:"error,omitempty"`
	Messages []MessageData `json:"messages,omitempty"`
}
type MessageData struct {
	Ts   string `json:"ts,omitempty"`
	Text string `json:"text,omitempty"`
}

func (c *Client) doRequest(req *http.Request) ([]byte, error) {
	resRaw, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resRaw.Body.Close()

	body, err := io.ReadAll(resRaw.Body)
	if err != nil {
		return nil, err
	}

	if resRaw.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status: %d, body: %s", resRaw.StatusCode, body)
	}
	return body, err
}

func (c *Client) SendMessage(channel_ID, text string) (*Response, error) {

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/api/chat.postMessage", c.Host), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.Token))

	q := req.URL.Query()
	q.Add("channel", channel_ID)
	q.Add("text", text)
	req.URL.RawQuery = q.Encode()

	body, err := c.doRequest(req)
	if err != nil {
		return nil, err
	}

	res := Response{}
	err = json.Unmarshal(body, &res)
	if err != nil {
		return nil, err
	}

	return &res, nil
}

func (c *Client) ReadMessage(channel_ID, ts string) (*Response, error) {

	req, err := http.NewRequest("GET", fmt.Sprintf("%s/api/conversations.replies", c.Host), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.Token))

	q := req.URL.Query()
	q.Add("channel", channel_ID)
	q.Add("ts", ts)
	req.URL.RawQuery = q.Encode()

	body, err := c.doRequest(req)
	if err != nil {
		return nil, err
	}

	res := Response{}
	err = json.Unmarshal(body, &res)
	if err != nil {
		return nil, err
	}

	return &res, nil
}

func (c *Client) UpdateMessage(channel_ID, ts, text string) (*Response, error) {

	req, err := http.NewRequest("GET", fmt.Sprintf("%s/api/chat.update", c.Host), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.Token))

	q := req.URL.Query()
	q.Add("channel", channel_ID)
	q.Add("ts", ts)
	q.Add("text", text)
	req.URL.RawQuery = q.Encode()

	body, err := c.doRequest(req)
	if err != nil {
		return nil, err
	}

	res := Response{}
	err = json.Unmarshal(body, &res)
	if err != nil {
		return nil, err
	}

	return &res, nil
}

func (c *Client) DeleteMessage(channel_ID, ts string) error {
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/api/chat.delete", c.Host), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.Token))

	q := req.URL.Query()
	q.Add("channel", channel_ID)
	q.Add("ts", ts)
	req.URL.RawQuery = q.Encode()

	_, err = c.doRequest(req)
	if err != nil {
		return err
	}

	return nil
}
