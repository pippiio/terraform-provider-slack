package slackclient

import (
	"net/http"
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
