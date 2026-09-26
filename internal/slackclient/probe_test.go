package slackclient

// Story: FR-11 / Task 1.5 -- the A-2 probe
//
// Input:  a real Slack bot token and a channel ID, supplied via environment.
// Process:
//   1. Post a throwaway message with chat.postMessage (POST -- known good).
//   2. Call chat.update with GET, exactly as requests.go:104 does today, and print
//      the raw response body.
//   3. Call chat.delete with GET, exactly as requests.go:131 does today, and print
//      the raw response body.
//   4. Report, per call, whether Slack answered {"ok":false}.
// Output: a verdict printed to the test log, to be recorded in spec.md.
//
// Why this exists: doRequest only checks the HTTP status (architecture.md I-1), so
// nobody knows whether chat.update / chat.delete currently work over GET. If they do
// not, they have been silent no-ops and the A-1 fix converts them into hard apply
// failures -- breaking terraform destroy for every existing consumer. Phase 2 must
// not start until this is answered.
//
// This probe reads the raw body itself rather than going through doRequest, because
// doRequest currently discards exactly the signal we are looking for.
//
// SAFETY: this posts a real message to the given channel and then attempts to delete
// it. Use a scratch channel. Skipped entirely unless both env vars are set.
//
// Run with:
//   SLACK_PROBE_TOKEN=xoxb-... SLACK_PROBE_CHANNEL=C0123456789 \
//     go test ./internal/slackclient/ -run TestProbe_A2 -v

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestProbe_A2_MutatingVerbs(t *testing.T) {
	token := os.Getenv("SLACK_PROBE_TOKEN")
	channel := os.Getenv("SLACK_PROBE_CHANNEL")
	if token == "" || channel == "" {
		t.Skip("A-2 probe skipped: set SLACK_PROBE_TOKEN and SLACK_PROBE_CHANNEL to run")
	}

	host := os.Getenv("SLACK_PROBE_HOST")
	if host == "" {
		host = "https://slack.com"
	}

	httpClient := &http.Client{Timeout: 10 * time.Second}

	// call issues a request the same way requests.go does -- query-string args, nil
	// body, bearer header -- and returns the status, raw body, and Slack's ok flag.
	call := func(method, endpoint string, params map[string]string) (int, string, bool, string) {
		t.Helper()
		req, err := http.NewRequest(method, fmt.Sprintf("%s/api/%s", host, endpoint), nil)
		if err != nil {
			t.Fatalf("building request for %s: %v", endpoint, err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		q := req.URL.Query()
		for k, v := range params {
			q.Add(k, v)
		}
		req.URL.RawQuery = q.Encode()

		res, err := httpClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, endpoint, err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)

		var env struct {
			Ok    bool   `json:"ok"`
			Error string `json:"error"`
		}
		_ = json.Unmarshal(body, &env)
		return res.StatusCode, string(body), env.Ok, env.Error
	}

	// Step 1: post a message with POST (the verb we believe is correct).
	status, body, ok, errCode := call(http.MethodPost, "chat.postMessage", map[string]string{
		"channel": channel,
		"text":    "draft A-2 probe -- safe to ignore, will self-delete",
	})
	t.Logf("chat.postMessage POST -> status=%d ok=%v error=%q", status, ok, errCode)
	if !ok {
		t.Fatalf("setup failed: could not post probe message. body=%s", body)
	}

	var posted struct {
		Ts      string `json:"ts"`
		Channel string `json:"channel"`
	}
	if err := json.Unmarshal([]byte(body), &posted); err != nil {
		t.Fatalf("decoding postMessage response: %v", err)
	}

	// Step 2: chat.update over GET, exactly as requests.go:104 does today.
	status, body, ok, errCode = call(http.MethodGet, "chat.update", map[string]string{
		"channel": posted.Channel,
		"ts":      posted.Ts,
		"text":    "draft A-2 probe -- updated",
	})
	t.Logf("chat.update  GET  -> status=%d ok=%v error=%q", status, ok, errCode)
	t.Logf("chat.update  GET  raw body: %s", body)
	updateWorks := ok

	// Step 3: chat.delete over GET, exactly as requests.go:131 does today.
	status, body, ok, errCode = call(http.MethodGet, "chat.delete", map[string]string{
		"channel": posted.Channel,
		"ts":      posted.Ts,
	})
	t.Logf("chat.delete  GET  -> status=%d ok=%v error=%q", status, ok, errCode)
	t.Logf("chat.delete  GET  raw body: %s", body)
	deleteWorks := ok

	// Cleanup: if GET delete did not work, delete with POST so we leave nothing behind.
	if !deleteWorks {
		status, body, ok, errCode = call(http.MethodPost, "chat.delete", map[string]string{
			"channel": posted.Channel,
			"ts":      posted.Ts,
		})
		t.Logf("chat.delete  POST -> status=%d ok=%v error=%q (cleanup)", status, ok, errCode)
		if !ok {
			t.Logf("WARNING: probe message %s could not be deleted; remove it manually. body=%s", posted.Ts, body)
		}
	}

	t.Log("=======================================================================")
	t.Logf("A-2 PROBE VERDICT: chat.update over GET works = %v", updateWorks)
	t.Logf("A-2 PROBE VERDICT: chat.delete over GET works = %v", deleteWorks)
	if updateWorks && deleteWorks {
		t.Log("=> A-2 is NOT a live failure. Keep it a Non-Goal; delete plan Task 2.8.")
	} else {
		t.Log("=> A-2 IS a live failure: these calls have been silent no-ops.")
		t.Log("=> The A-1 fix will turn them into hard errors. Task 2.8 is REQUIRED:")
		t.Log("=> change chat.update and chat.delete to POST in this same release.")
	}
	t.Log("Record this verdict in draft/tracks/slack-user-data-source/spec.md (FR-11).")
	t.Log("=======================================================================")
}
