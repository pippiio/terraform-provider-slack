package slackclient

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type UserReponse struct {
	Members []MembersData `json:"members"`
}

type MembersData struct {
	Id   string `json:"id"`
	Name string `json:"name"`
}

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

// doRequest issues req and returns the raw response body.
//
// It fails on three conditions, in order:
//   1. transport failure           -> the underlying error
//   2. non-200 HTTP status         -> a plain error naming the status
//   3. HTTP 200 with ok:false      -> a *SlackError carrying Slack's error code
//
// The third case is the important one. The Slack Web API reports application-level
// failures -- invalid auth, missing scope, unknown user, rate limiting -- as HTTP 200
// with {"ok": false, "error": "..."} in the body. Checking only the status code makes
// those failures look like successes, which is how a terraform apply could report
// success while doing nothing at all.
//
// doRequest is the single choke point for every Slack call in this package (fanIn=5),
// so this check covers all current and future methods.
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

	// Ok is a pointer so an absent field is distinguishable from an explicit false.
	// A body we cannot parse, or one carrying no "ok" field, is passed through
	// untouched: the caller's own json.Unmarshal is better placed to report it than a
	// synthesised error here would be.
	var envelope struct {
		Ok    *bool  `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil && envelope.Ok != nil && !*envelope.Ok {
		return nil, &SlackError{Code: envelope.Error, Endpoint: endpointName(req)}
	}

	return body, nil
}

// endpointName extracts the Slack method name from a request URL, so errors can say
// which call failed. "https://slack.com/api/users.info" -> "users.info".
func endpointName(req *http.Request) string {
	if req == nil || req.URL == nil {
		return ""
	}
	return strings.TrimPrefix(req.URL.Path, "/api/")
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

func (c *Client) ReadUserIds() (*UserReponse, error) {

	req, err := http.NewRequest("GET", fmt.Sprintf("%s/api/users.list", c.Host), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.Token))

	body, err := c.doRequest(req)
	if err != nil {
		return nil, err
	}

	res := UserReponse{}
	err = json.Unmarshal(body, &res)
	if err != nil {
		return nil, err
	}

	return &res, nil
}

// GetUserByID looks a user up by Slack user ID via users.info.
//
// Requires the users:read scope. The users:read.email scope additionally governs
// whether profile.email is present in the response; without it the field is omitted
// rather than the call failing.
func (c *Client) GetUserByID(userID string) (*User, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/api/users.info", c.Host), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.Token))

	q := req.URL.Query()
	q.Add("user", userID)
	req.URL.RawQuery = q.Encode()

	body, err := c.doRequest(req)
	if err != nil {
		return nil, err
	}

	res := userResponse{}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, err
	}

	return &res.User, nil
}

// GetUserByEmail looks a user up by email address via users.lookupByEmail.
//
// Requires the users:read.email scope; without it Slack answers missing_scope, which
// doRequest surfaces as a *SlackError so the caller can name the scope.
//
// Note this endpoint does not find deactivated accounts, whereas users.info returns
// them with deleted:true. That asymmetry is Slack's.
func (c *Client) GetUserByEmail(email string) (*User, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/api/users.lookupByEmail", c.Host), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.Token))

	q := req.URL.Query()
	q.Add("email", email)
	req.URL.RawQuery = q.Encode()

	body, err := c.doRequest(req)
	if err != nil {
		return nil, err
	}

	res := userResponse{}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, err
	}

	return &res.User, nil
}
