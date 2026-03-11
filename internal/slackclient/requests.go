package slackclient

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type Response struct {
	Ts string `json:"ts,omitempty"`
}

func (c *Client) SendMessage(message, slack_ID string) (*Response, error) {

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/api/chat.postMessage", c.Host), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.Token))

	q := req.URL.Query()
	q.Add("channel", slack_ID)
	q.Add("text", message)
	req.URL.RawQuery = q.Encode()

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

	res := Response{}
	err = json.Unmarshal(body, &res)
	if err != nil {
		return nil, err
	}

	return &res, nil
}
