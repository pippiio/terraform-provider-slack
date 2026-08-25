package slackclient

import (
	"errors"
	"net/http"
	"strings"
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
