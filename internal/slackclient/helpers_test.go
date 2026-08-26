package slackclient

// Story: slackclient test harness
//
// Input:  a map of Slack endpoint paths to canned responses (recorded fixtures or
//         literal bodies), supplied by a test.
// Process:
//   1. Start an httptest server whose handler looks the request path up in the map.
//   2. Record every inbound request (method, path, query, auth header, body) so the
//      test can assert on how the client called Slack, not just what it returned.
//   3. Serve the mapped response; answer unmapped paths with a 404 so a typo in a
//      test surfaces loudly instead of silently passing.
//   4. Build a real *Client via NewClient pointed at the server URL.
// Output: a *Client wired to the stub, plus a *recorder for assertions.
//
// Dependencies: net/http/httptest, testdata/*.json fixtures.
// Side effects: starts a local TCP listener, closed via t.Cleanup.
//
// This exists because Client.Host is injectable with no default (architecture.md I-5),
// so the whole Slack surface can be tested without a workspace or TF_ACC.

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// stubResponse describes what the stub server returns for one endpoint.
type stubResponse struct {
	status  int
	fixture string // filename under testdata/, takes precedence over body
	body    string // literal response body
}

// routes maps a Slack endpoint path (e.g. "/api/users.info") to its stub response.
type routes map[string]stubResponse

// fixture serves the named file from testdata/ with HTTP 200.
func fixture(name string) stubResponse {
	return stubResponse{status: http.StatusOK, fixture: name}
}

// raw serves a literal body with an explicit status, for cases no fixture covers.
func raw(status int, body string) stubResponse {
	return stubResponse{status: status, body: body}
}

// recordedRequest is one request the stub server received.
type recordedRequest struct {
	Method        string
	Path          string
	Query         url.Values
	Authorization string
	Body          string
}

// recorder collects requests for assertion. Safe for concurrent handler goroutines.
type recorder struct {
	mu   sync.Mutex
	reqs []recordedRequest
}

func (r *recorder) add(req recordedRequest) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reqs = append(r.reqs, req)
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.reqs)
}

// last returns the most recent request, or the zero value if none were recorded.
func (r *recorder) last() recordedRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.reqs) == 0 {
		return recordedRequest{}
	}
	return r.reqs[len(r.reqs)-1]
}

// all returns a copy of every recorded request, in arrival order.
func (r *recorder) all() []recordedRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]recordedRequest, len(r.reqs))
	copy(out, r.reqs)
	return out
}

// newTestClient starts a stub Slack server for the given routes and returns a Client
// pointed at it. The server is closed automatically when the test finishes.
func newTestClient(t *testing.T, rt routes) (*Client, *recorder) {
	t.Helper()

	rec := &recorder{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		bodyBytes, _ := io.ReadAll(req.Body)
		rec.add(recordedRequest{
			Method:        req.Method,
			Path:          req.URL.Path,
			Query:         req.URL.Query(),
			Authorization: req.Header.Get("Authorization"),
			Body:          string(bodyBytes),
		})

		stub, ok := rt[req.URL.Path]
		if !ok {
			// Unmapped path: fail loudly rather than returning something plausible.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"ok":false,"error":"unknown_method"}`))
			return
		}

		payload := []byte(stub.body)
		if stub.fixture != "" {
			b, err := os.ReadFile(filepath.Join("testdata", stub.fixture))
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"ok":false,"error":"fixture_read_failed"}`))
				return
			}
			payload = b
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(stub.status)
		_, _ = w.Write(payload)
	}))
	t.Cleanup(srv.Close)

	host := srv.URL
	token := "xoxb-test-token"
	c, err := NewClient(&host, &token)
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	return c, rec
}

// mustRequest builds a request or fails the test.
func mustRequest(t *testing.T, method, rawURL string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, rawURL, nil)
	if err != nil {
		t.Fatalf("http.NewRequest(%s, %s): %v", method, rawURL, err)
	}
	return req
}
