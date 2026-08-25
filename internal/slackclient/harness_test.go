package slackclient

import (
	"net/http"
	"testing"
)

// TestHarness_ServesFixture proves the httptest harness wires a *Client to a stub
// server that serves recorded Slack JSON by endpoint path, and records the requests
// it received so tests can assert on HTTP verb and query parameters.
func TestHarness_ServesFixture(t *testing.T) {
	c, rec := newTestClient(t, routes{
		"/api/users.info": fixture("users_info_full.json"),
	})

	body, err := c.doRequest(mustRequest(t, http.MethodGet, c.Host+"/api/users.info?user=W012A3CDE"))
	if err != nil {
		t.Fatalf("doRequest returned error: %v", err)
	}
	if len(body) == 0 {
		t.Fatal("expected fixture body, got empty response")
	}

	if got := rec.count(); got != 1 {
		t.Fatalf("expected 1 recorded request, got %d", got)
	}
	last := rec.last()
	if last.Method != http.MethodGet {
		t.Errorf("method = %q, want %q", last.Method, http.MethodGet)
	}
	if got := last.Query.Get("user"); got != "W012A3CDE" {
		t.Errorf("query user = %q, want %q", got, "W012A3CDE")
	}
	if last.Path != "/api/users.info" {
		t.Errorf("path = %q, want %q", last.Path, "/api/users.info")
	}
}

// TestHarness_AuthorizationHeader proves the client sends the bearer token so tests
// covering auth behaviour have a trustworthy foundation.
func TestHarness_AuthorizationHeader(t *testing.T) {
	c, rec := newTestClient(t, routes{
		"/api/users.info": fixture("users_info_full.json"),
	})

	req := mustRequest(t, http.MethodGet, c.Host+"/api/users.info")
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if _, err := c.doRequest(req); err != nil {
		t.Fatalf("doRequest returned error: %v", err)
	}

	if got := rec.last().Authorization; got != "Bearer "+c.Token {
		t.Errorf("Authorization = %q, want %q", got, "Bearer "+c.Token)
	}
}
