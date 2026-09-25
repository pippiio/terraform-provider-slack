package slackclient

// Story: Task 1.3 -- the token-scopes probe
//
// Input:  a real Slack token, supplied via environment.
// Process:
//   1. Call three cheap read endpoints and capture X-OAuth-Scopes and
//      X-Accepted-OAuth-Scopes from each response.
//   2. Report whether the header is present at all, and whether it is identical
//      across endpoints -- FR-9 caches it once and reuses it, which is only sound
//      if it does not vary per call.
//   3. Independently, read one page of users.list and count how many users carry
//      profile.email.
//   4. Correlate the two. The header says whether users:read.email was granted;
//      the email count says whether Slack is actually returning emails. FR-9's
//      authoritative path and its behavioural fallback must agree.
// Output: a verdict printed to the test log, to be recorded in spec.md.
//
// READ-ONLY. Creates nothing, deletes nothing.
//
// Why this matters more than it looks: FR-9 fails a read early, before scanning the
// remaining users.list pages, when it believes the scope is missing. If the header is
// absent, unreliable, or varies per endpoint, that shortcut is wrong and the design
// falls back to inference. Getting this wrong means either a spurious hard failure on
// a correctly-scoped token, or a wasted full-workspace scan on a broken one.
//
// Run with:
//   SLACK_PROBE_TOKEN=xoxb-... go test ./internal/slackclient/ -run TestProbe_TokenScopes -v

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
)

// scopeHeader is the response header Slack is documented to return, listing the
// scopes actually granted to the token. acceptedScopeHeader lists what the endpoint
// would accept, which is useful for diagnostics but not for FR-9.
const (
	scopeHeader         = "X-Oauth-Scopes"
	acceptedScopeHeader = "X-Accepted-Oauth-Scopes"
	emailScope          = "users:read.email"
)

func TestProbe_TokenScopes(t *testing.T) {
	token := os.Getenv("SLACK_PROBE_TOKEN")
	if token == "" {
		t.Skip("token-scopes probe skipped: set SLACK_PROBE_TOKEN to run")
	}

	host := os.Getenv("SLACK_PROBE_HOST")
	if host == "" {
		host = "https://slack.com"
	}

	httpClient := &http.Client{Timeout: 10 * time.Second}

	call := func(endpoint string, params map[string]string) (http.Header, string) {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/%s", host, endpoint), nil)
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
			t.Fatalf("GET %s: %v", endpoint, err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		return res.Header, string(body)
	}

	// --- Steps 1 & 2: is the header there, and is it stable? -----------------------
	endpoints := []struct {
		name   string
		params map[string]string
	}{
		{"auth.test", nil},
		{"users.list", map[string]string{"limit": "1"}},
		{"conversations.list", map[string]string{"limit": "1"}},
	}

	seen := map[string]string{}
	for _, e := range endpoints {
		h, _ := call(e.name, e.params)
		granted := h.Get(scopeHeader)
		accepted := h.Get(acceptedScopeHeader)
		seen[e.name] = granted

		t.Logf("%-20s %s = %q", e.name, scopeHeader, granted)
		t.Logf("%-20s %s = %q", e.name, acceptedScopeHeader, accepted)
	}

	present := false
	for _, v := range seen {
		if v != "" {
			present = true
		}
	}

	// FR-9 records the header once and reuses it. If two endpoints disagree about what
	// the token was granted, a single cached value is not safe to branch on.
	distinct := map[string]bool{}
	for _, v := range seen {
		if v != "" {
			distinct[v] = true
		}
	}
	consistent := len(distinct) <= 1

	headerSaysEmail := false
	for _, v := range seen {
		for _, s := range strings.Split(v, ",") {
			if strings.TrimSpace(s) == emailScope {
				headerSaysEmail = true
			}
		}
	}

	// --- Steps 3 & 4: does behaviour agree with the header? ------------------------
	_, body := call("users.list", map[string]string{"limit": "50"})
	var ul struct {
		Ok      bool   `json:"ok"`
		Error   string `json:"error"`
		Members []struct {
			Name    string `json:"name"`
			Profile struct {
				Email *string `json:"email"`
			} `json:"profile"`
		} `json:"members"`
	}
	if err := json.Unmarshal([]byte(body), &ul); err != nil {
		t.Fatalf("decoding users.list: %v", err)
	}
	if !ul.Ok {
		t.Fatalf("users.list failed: %q -- cannot correlate", ul.Error)
	}

	withEmail := 0
	for _, m := range ul.Members {
		if m.Profile.Email != nil && *m.Profile.Email != "" {
			withEmail++
		}
	}
	behaviourSaysEmail := withEmail > 0
	t.Logf("users.list sample: %d of %d members carry profile.email", withEmail, len(ul.Members))

	// --- Verdict --------------------------------------------------------------------
	names := make([]string, 0, len(seen))
	for k := range seen {
		names = append(names, k)
	}
	sort.Strings(names)

	t.Log("=======================================================================")
	t.Logf("PROBE VERDICT: %s present                = %v", scopeHeader, present)
	t.Logf("PROBE VERDICT: identical across endpoints = %v", consistent)
	t.Logf("PROBE VERDICT: header grants %-14s = %v", emailScope, headerSaysEmail)
	t.Logf("PROBE VERDICT: emails actually returned   = %v", behaviourSaysEmail)

	switch {
	case !present:
		t.Log("=> FR-9's authoritative path is NOT available. Slack did not return the")
		t.Log("=> header. Build the behavioural fallback only, and record that here so")
		t.Log("=> nobody re-adds the header path on the strength of the documentation.")
	case !consistent:
		t.Log("=> The header VARIES by endpoint. Do not cache one value on the Client;")
		t.Log("=> either record it per endpoint or drop the authoritative path.")
		for _, n := range names {
			t.Logf("=>   %-20s %q", n, seen[n])
		}
	case headerSaysEmail != behaviourSaysEmail:
		t.Log("=> The header and observed behaviour DISAGREE. This is the case FR-9")
		t.Log("=> must not get wrong: one of the two signals is lying.")
		t.Logf("=>   header says granted=%v, users.list returned emails=%v", headerSaysEmail, behaviourSaysEmail)
		t.Log("=> If header=true and emails=false, the behavioural fallback would raise a")
		t.Log("=> false scope error. Prefer the header and delete the fallback.")
		t.Log("=> If header=false and emails=true, the header path would fail a working")
		t.Log("=> config. Delete the header path.")
		t.Log("=> Note spec Q3: emails can also be absent per-user for other reasons, so")
		t.Log("=> re-run against a workspace where users are known to have emails set.")
	default:
		t.Log("=> FR-9 is SOUND as specified. Header present, stable, and in agreement")
		t.Log("=> with observed behaviour. Implement the authoritative path with the")
		t.Log("=> behavioural fallback for the unknown case.")
	}
	t.Log("Record this verdict in draft/tracks/slack-users-data-source/spec.md (FR-13).")
	t.Log("=======================================================================")
}
