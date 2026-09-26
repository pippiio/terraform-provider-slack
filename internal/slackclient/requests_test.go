package slackclient

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// TestSendMessage_Smoke proves an existing production client method works end to end
// against the stub server: correct endpoint, correct verb, bearer auth, query params,
// and a decoded response. This is the baseline the doRequest change must not regress.
func TestSendMessage_Smoke(t *testing.T) {
	c, rec := newTestClient(t, routes{
		"/api/chat.postMessage": fixture("chat_postmessage_ok.json"),
	})

	res, err := c.SendMessage("C123456789", "Here's a message for you")
	if err != nil {
		t.Fatalf("SendMessage returned error: %v", err)
	}

	if res.Ts != "1503435956.000247" {
		t.Errorf("Ts = %q, want %q", res.Ts, "1503435956.000247")
	}
	if res.Channel != "C123456789" {
		t.Errorf("Channel = %q, want %q", res.Channel, "C123456789")
	}

	req := rec.last()
	if req.Path != "/api/chat.postMessage" {
		t.Errorf("path = %q, want /api/chat.postMessage", req.Path)
	}
	if req.Method != http.MethodPost {
		t.Errorf("method = %q, want POST", req.Method)
	}
	if got := req.Query.Get("channel"); got != "C123456789" {
		t.Errorf("query channel = %q, want C123456789", got)
	}
	if got := req.Query.Get("text"); got != "Here's a message for you" {
		t.Errorf("query text = %q, want the message text", got)
	}
	if got := req.Authorization; got != "Bearer xoxb-test-token" {
		t.Errorf("Authorization = %q, want bearer token", got)
	}
}

// TestReadUserIds_Smoke covers the second existing method through the harness.
func TestReadUserIds_Smoke(t *testing.T) {
	c, rec := newTestClient(t, routes{
		"/api/users.list": fixture("users_list_ok.json"),
	})

	res, err := c.ReadUserIds()
	if err != nil {
		t.Fatalf("ReadUserIds returned error: %v", err)
	}

	if len(res.Members) != 2 {
		t.Fatalf("len(Members) = %d, want 2", len(res.Members))
	}
	if res.Members[0].Id != "W012A3CDE" || res.Members[0].Name != "spengler" {
		t.Errorf("Members[0] = %+v, want {W012A3CDE spengler}", res.Members[0])
	}
	if rec.last().Path != "/api/users.list" {
		t.Errorf("path = %q, want /api/users.list", rec.last().Path)
	}
}

// --- doRequest: A-1 (Slack ok:false must surface as *SlackError) ---

// TestDoRequest_OkFalseReturnsSlackError is the core A-1 test. Slack answers HTTP 200
// with {"ok":false,...} for application failures; before this change doRequest returned
// the body with a nil error and the caller reported success.
func TestDoRequest_OkFalseReturnsSlackError(t *testing.T) {
	for _, code := range []string{"users_not_found", "missing_scope", "invalid_auth", "ratelimited"} {
		t.Run(code, func(t *testing.T) {
			c, _ := newTestClient(t, routes{
				"/api/users.info": fixture("err_" + code + ".json"),
			})

			body, err := c.doRequest(mustRequest(t, http.MethodGet, c.Host+"/api/users.info"))
			if err == nil {
				t.Fatalf("expected an error for ok:false, got nil (body=%s)", body)
			}

			var se *SlackError
			if !errors.As(err, &se) {
				t.Fatalf("error is %T (%v), want *SlackError", err, err)
			}
			if se.Code != code {
				t.Errorf("Code = %q, want %q", se.Code, code)
			}
			if se.Endpoint != "users.info" {
				t.Errorf("Endpoint = %q, want %q", se.Endpoint, "users.info")
			}
		})
	}
}

// TestDoRequest_OkTruePassesThrough proves the success path is untouched.
func TestDoRequest_OkTruePassesThrough(t *testing.T) {
	c, _ := newTestClient(t, routes{
		"/api/chat.postMessage": fixture("chat_postmessage_ok.json"),
	})

	body, err := c.doRequest(mustRequest(t, http.MethodPost, c.Host+"/api/chat.postMessage"))
	if err != nil {
		t.Fatalf("unexpected error on ok:true response: %v", err)
	}
	if !strings.Contains(string(body), "1503435956.000247") {
		t.Errorf("body did not pass through intact: %s", body)
	}
}

// TestDoRequest_NonOKStatusKeepsExistingBehaviour guards the pre-existing HTTP-status
// error path, which must stay a plain error and not become a SlackError.
func TestDoRequest_NonOKStatusKeepsExistingBehaviour(t *testing.T) {
	c, _ := newTestClient(t, routes{
		"/api/users.info": raw(http.StatusInternalServerError, `{"ok":false,"error":"internal_error"}`),
	})

	_, err := c.doRequest(mustRequest(t, http.MethodGet, c.Host+"/api/users.info"))
	if err == nil {
		t.Fatal("expected an error for HTTP 500")
	}
	var se *SlackError
	if errors.As(err, &se) {
		t.Errorf("HTTP-status failure became a *SlackError; want a plain transport error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error %q should mention the status code", err.Error())
	}
}

// TestDoRequest_NonJSONBodyPassesThrough proves an unparseable body is not swallowed as
// a false success or a bogus SlackError -- it reaches the caller, whose json.Unmarshal
// produces the real error. Preserves existing behaviour.
func TestDoRequest_NonJSONBodyPassesThrough(t *testing.T) {
	c, _ := newTestClient(t, routes{
		"/api/users.info": raw(http.StatusOK, `<html>not json</html>`),
	})

	body, err := c.doRequest(mustRequest(t, http.MethodGet, c.Host+"/api/users.info"))
	if err != nil {
		t.Fatalf("unparseable body should pass through, got error: %v", err)
	}
	if string(body) != `<html>not json</html>` {
		t.Errorf("body = %q, want it passed through unchanged", body)
	}
}

// --- NFR-3 / AC-12: every existing method must surface ok:false, not swallow it ---
//
// Testing doRequest alone does not prove its callers behave correctly once it starts
// returning errors. These five cover the actual public methods.

func TestSendMessage_OkFalseSurfacesError(t *testing.T) {
	c, _ := newTestClient(t, routes{
		"/api/chat.postMessage": fixture("err_invalid_auth.json"),
	})

	res, err := c.SendMessage("C123456789", "hello")
	if err == nil {
		t.Fatalf("expected error, got nil with res=%+v", res)
	}
	if res != nil {
		t.Errorf("res = %+v, want nil on error", res)
	}
	if got := ErrorCode(err); got != "invalid_auth" {
		t.Errorf("ErrorCode = %q, want invalid_auth", got)
	}
}

func TestReadMessage_OkFalseSurfacesError(t *testing.T) {
	c, _ := newTestClient(t, routes{
		"/api/conversations.replies": fixture("err_thread_not_found.json"),
	})

	res, err := c.ReadMessage("C123456789", "1503435956.000247")
	if err == nil {
		t.Fatalf("expected error, got nil with res=%+v", res)
	}
	if got := ErrorCode(err); got != "thread_not_found" {
		t.Errorf("ErrorCode = %q, want thread_not_found", got)
	}
}

func TestUpdateMessage_OkFalseSurfacesError(t *testing.T) {
	c, _ := newTestClient(t, routes{
		"/api/chat.update": fixture("err_invalid_auth.json"),
	})

	res, err := c.UpdateMessage("C123456789", "1503435956.000247", "updated")
	if err == nil {
		t.Fatalf("expected error, got nil with res=%+v", res)
	}
	if got := ErrorCode(err); got != "invalid_auth" {
		t.Errorf("ErrorCode = %q, want invalid_auth", got)
	}
}

func TestDeleteMessage_OkFalseSurfacesError(t *testing.T) {
	c, _ := newTestClient(t, routes{
		"/api/chat.delete": fixture("err_invalid_auth.json"),
	})

	err := c.DeleteMessage("C123456789", "1503435956.000247")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := ErrorCode(err); got != "invalid_auth" {
		t.Errorf("ErrorCode = %q, want invalid_auth", got)
	}
}

func TestReadUserIds_OkFalseSurfacesError(t *testing.T) {
	c, _ := newTestClient(t, routes{
		"/api/users.list": fixture("err_missing_scope.json"),
	})

	res, err := c.ReadUserIds()
	if err == nil {
		t.Fatalf("expected error, got nil with res=%+v", res)
	}
	if got := ErrorCode(err); got != "missing_scope" {
		t.Errorf("ErrorCode = %q, want missing_scope", got)
	}
}

// Guardrails A-8: users.list is paginated by Slack. Reading only the first page makes
// every member past it invisible, which slack_user_ids then reports as an unresolved
// username -- a hard error since the fix above, for an account that exists.
func TestReadUserIds_FollowsCursorToLaterPages(t *testing.T) {
	c, rec := newTestClient(t, routes{
		"/api/users.list": sequence(
			fixture("users_list_page1.json"),
			fixture("users_list_page2.json"),
		),
	})

	res, err := c.ReadUserIds()
	if err != nil {
		t.Fatalf("ReadUserIds returned error: %v", err)
	}

	got := map[string]string{}
	for _, m := range res.Members {
		got[m.Name] = m.Id
	}
	if len(got) != 2 {
		t.Fatalf("collected %d members (%v), want both pages", len(got), got)
	}
	if got["spengler"] != "W012A3CDE" {
		t.Errorf("spengler = %q, want W012A3CDE from page 1", got["spengler"])
	}
	if got["glinda"] != "W07QCRPA4" {
		t.Errorf("glinda = %q, want W07QCRPA4 from page 2", got["glinda"])
	}

	reqs := rec.all()
	if len(reqs) != 2 {
		t.Fatalf("made %d requests, want 2 (one per page)", len(reqs))
	}
	if got := reqs[0].Query.Get("limit"); got == "" {
		t.Error("users.list must be called with an explicit limit")
	}
	if got := reqs[1].Query.Get("cursor"); got != "dXNlcjpVMDYxTkZUVDI=" {
		t.Errorf("second request cursor = %q, want the next_cursor from page 1", got)
	}
}

// A cursor that never empties must not loop forever.
func TestReadUserIds_StopsOnRepeatedCursor(t *testing.T) {
	c, rec := newTestClient(t, routes{
		"/api/users.list": fixture("users_list_page1.json"),
	})

	if _, err := c.ReadUserIds(); err != nil {
		t.Fatalf("ReadUserIds returned error: %v", err)
	}
	if got := rec.count(); got > 20 {
		t.Errorf("made %d requests, want a bounded scan", got)
	}
}

// FR-9: the client records the scopes Slack reports, so the provider can tell "this
// token cannot see emails" from "this user has no email". The distinction that matters
// is three-way, not two: granted, not granted, and not yet known.

func TestClient_ScopesUnknownBeforeAnyCall(t *testing.T) {
	c, _ := newTestClient(t, routes{})

	_, known := c.GrantedScopes()
	if known {
		t.Error("scopes must be unknown before any call; FR-9 falls back to inference when they are")
	}
}

func TestDoRequest_RecordsOAuthScopes(t *testing.T) {
	c, _ := newTestClient(t, routes{
		"/api/users.list": withHeaders(
			fixture("users_list_ok.json"),
			map[string]string{"X-OAuth-Scopes": "users:read,users:read.email,chat:write"},
		),
	})

	if _, err := c.ReadUserIds(); err != nil {
		t.Fatalf("ReadUserIds returned error: %v", err)
	}

	scopes, known := c.GrantedScopes()
	if !known {
		t.Fatal("scopes must be known after a response carrying the header")
	}
	for _, want := range []string{"users:read", "users:read.email", "chat:write"} {
		if !scopes[want] {
			t.Errorf("scope %q missing from %v", want, scopes)
		}
	}
	if scopes["usergroups:write"] {
		t.Error("a scope Slack did not grant must not appear")
	}
}

// A token genuinely without the scope is the case FR-9 fails a read on, so "known and
// absent" has to be distinguishable from "unknown".
func TestDoRequest_RecordsScopesWithoutEmailScope(t *testing.T) {
	c, _ := newTestClient(t, routes{
		"/api/users.list": withHeaders(
			fixture("users_list_ok.json"),
			map[string]string{"X-OAuth-Scopes": "users:read"},
		),
	})

	if _, err := c.ReadUserIds(); err != nil {
		t.Fatalf("ReadUserIds returned error: %v", err)
	}

	scopes, known := c.GrantedScopes()
	if !known {
		t.Fatal("a header listing one scope still means the scopes are known")
	}
	if scopes["users:read.email"] {
		t.Error("users:read.email must not be reported as granted")
	}
}

// If Slack sends no header, the probe's negative case, scopes stay unknown and the
// provider must not conclude the scope is missing.
func TestDoRequest_NoScopeHeaderLeavesScopesUnknown(t *testing.T) {
	c, _ := newTestClient(t, routes{
		"/api/users.list": fixture("users_list_ok.json"),
	})

	if _, err := c.ReadUserIds(); err != nil {
		t.Fatalf("ReadUserIds returned error: %v", err)
	}

	if _, known := c.GrantedScopes(); known {
		t.Error("no header must leave scopes unknown, not empty-and-known")
	}
}

// Slack's own examples space-pad the list; a scope must not be recorded as " chat:write".
func TestDoRequest_ScopeHeaderIsTrimmed(t *testing.T) {
	c, _ := newTestClient(t, routes{
		"/api/users.list": withHeaders(
			fixture("users_list_ok.json"),
			map[string]string{"X-OAuth-Scopes": "users:read, users:read.email , chat:write"},
		),
	})

	if _, err := c.ReadUserIds(); err != nil {
		t.Fatalf("ReadUserIds returned error: %v", err)
	}

	scopes, _ := c.GrantedScopes()
	if !scopes["users:read.email"] {
		t.Errorf("padded scope was not trimmed: %v", scopes)
	}
}

// The scope fields are the client's only mutable state, written from doRequest and read
// from the provider. One apply resolves many data sources, so both happen concurrently.
// Without this the race detector has nothing to detect: no other test calls doRequest
// from more than one goroutine.
func TestDoRequest_ScopeRecordingIsRaceFree(t *testing.T) {
	c, _ := newTestClient(t, routes{
		"/api/users.list": withHeaders(
			fixture("users_list_ok.json"),
			map[string]string{"X-OAuth-Scopes": "users:read,users:read.email"},
		),
	})

	const goroutines = 16
	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			if _, err := c.ReadUserIds(); err != nil {
				t.Errorf("ReadUserIds: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			// Reading the returned map must be safe even while writers are running --
			// which is why GrantedScopes hands back a copy rather than the live map.
			scopes, known := c.GrantedScopes()
			if known {
				_ = scopes["users:read.email"]
			}
		}()
	}
	wg.Wait()

	scopes, known := c.GrantedScopes()
	if !known || !scopes["users:read.email"] {
		t.Errorf("after %d concurrent calls, scopes = %v known = %v", goroutines, scopes, known)
	}
}
