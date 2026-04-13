package slackclient

import (
	"net/http"
	"time"
)

type Client struct {
	HTTPClient *http.Client
	Host       string
	Token      string
}

func NewClient(host, token *string) (*Client, error) {
	c := Client{
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
		Host:       *host,
	}
	c.Token = *token

	return &c, nil
}
