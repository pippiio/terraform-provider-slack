package slackclient

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

type Client struct {
	HTTPClient *http.Client
	Host       string
	Token      string

	// scopeMu guards the two fields below. Every Slack response passes through
	// doRequest, which may run concurrently for different resources in one apply.
	scopeMu sync.RWMutex
	// scopes is the set Slack last reported as granted to this token.
	scopes map[string]bool
	// scopesKnown separates "Slack told us the token has no such scope" from "Slack
	// has not told us anything yet". The provider must not conclude a scope is missing
	// from silence -- see FR-9.
	scopesKnown bool
}

// oauthScopesHeader is how Slack reports the scopes granted to the calling token.
const oauthScopesHeader = "X-OAuth-Scopes"

// recordScopes captures the granted-scope header from a Slack response, if it carried
// one. Responses without the header leave the previous state untouched, so one endpoint
// that omits it cannot erase what another reported.
func (c *Client) recordScopes(h http.Header) {
	raw := h.Get(oauthScopesHeader)
	if raw == "" {
		return
	}

	scopes := make(map[string]bool)
	for _, s := range strings.Split(raw, ",") {
		if s = strings.TrimSpace(s); s != "" {
			scopes[s] = true
		}
	}

	c.scopeMu.Lock()
	defer c.scopeMu.Unlock()
	c.scopes = scopes
	c.scopesKnown = true
}

// GrantedScopes returns the scopes Slack reports for this token, and whether Slack has
// reported them at all.
//
// A false second return means "not known", never "none granted". Callers deciding
// whether to fail on a missing scope must branch on it: concluding absence from silence
// would fail a correctly-scoped configuration.
//
// The returned map is a copy; the caller cannot mutate the client's state through it.
func (c *Client) GrantedScopes() (map[string]bool, bool) {
	c.scopeMu.RLock()
	defer c.scopeMu.RUnlock()

	if !c.scopesKnown {
		return nil, false
	}

	out := make(map[string]bool, len(c.scopes))
	for k, v := range c.scopes {
		out[k] = v
	}
	return out, true
}

func NewClient(host, token *string) (*Client, error) {
	c := Client{
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
		Host:       *host,
	}
	c.Token = *token

	return &c, nil
}
