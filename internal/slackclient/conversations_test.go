package slackclient

// Tasks 2.1-2.3: conversations.members, the one genuinely new endpoint this track needs.
//
// The shape mirrors the scanUsers tests deliberately. Both are cursor-paginated list
// endpoints, and guardrail A-8 exists because the first one shipped unpaginated.

import (
	"net/http"
	"testing"
)

func TestListChannelMembers_ReturnsMembers(t *testing.T) {
	c, rec := newTestClient(t, routes{
		"/api/conversations.members": fixture("conversations_members_ok.json"),
	})

	members, err := c.ListChannelMembers("C012AB3CD")
	if err != nil {
		t.Fatalf("ListChannelMembers returned error: %v", err)
	}

	want := []string{"W012A3CDE", "W07QCRPA4", "B01BOT0001"}
	if len(members) != len(want) {
		t.Fatalf("got %d members (%v), want %d", len(members), members, len(want))
	}
	for i := range want {
		if members[i] != want[i] {
			t.Errorf("members[%d] = %q, want %q -- order must be preserved", i, members[i], want[i])
		}
	}

	req := rec.last()
	if req.Path != "/api/conversations.members" {
		t.Errorf("path = %q, want /api/conversations.members", req.Path)
	}
	if req.Method != http.MethodGet {
		t.Errorf("method = %q, want GET", req.Method)
	}
	if got := req.Query.Get("channel"); got != "C012AB3CD" {
		t.Errorf("query channel = %q, want C012AB3CD", got)
	}
	if got := req.Query.Get("limit"); got == "" {
		t.Error("conversations.members must be called with an explicit limit, so the scan is bounded per page")
	}
	if req.Authorization != "Bearer xoxb-test-token" {
		t.Errorf("Authorization = %q, want the bearer token", req.Authorization)
	}
}

// Guardrail A-8. A channel with more members than one page is ordinary, and reading
// only the first page would silently omit the rest -- which slack_users would then
// report as "not a member", for someone who is.
func TestListChannelMembers_FollowsCursorToLaterPages(t *testing.T) {
	c, rec := newTestClient(t, routes{
		"/api/conversations.members": sequence(
			fixture("conversations_members_page1.json"),
			fixture("conversations_members_page2.json"),
		),
	})

	members, err := c.ListChannelMembers("C012AB3CD")
	if err != nil {
		t.Fatalf("ListChannelMembers returned error: %v", err)
	}

	if len(members) != 2 {
		t.Fatalf("got %d members (%v), want both pages", len(members), members)
	}
	if members[0] != "W012A3CDE" || members[1] != "W07QCRPA4" {
		t.Errorf("members = %v, want page 1 then page 2 in order", members)
	}

	reqs := rec.all()
	if len(reqs) != 2 {
		t.Fatalf("made %d requests, want 2 (one per page)", len(reqs))
	}
	if got := reqs[0].Query.Get("cursor"); got != "" {
		t.Errorf("first request cursor = %q, want empty", got)
	}
	if got := reqs[1].Query.Get("cursor"); got != "dXNlcjpVMDYxTkZUVDI=" {
		t.Errorf("second request cursor = %q, want the next_cursor from page 1", got)
	}
	if got := reqs[1].Query.Get("channel"); got != "C012AB3CD" {
		t.Errorf("second request dropped the channel: %q", got)
	}
}

// A cursor Slack never empties would loop forever.
func TestListChannelMembers_StopsOnRepeatedCursor(t *testing.T) {
	c, rec := newTestClient(t, routes{
		"/api/conversations.members": fixture("conversations_members_page1.json"),
	})

	members, err := c.ListChannelMembers("C012AB3CD")
	if err != nil {
		t.Fatalf("ListChannelMembers returned error: %v", err)
	}
	if got := rec.count(); got > 20 {
		t.Errorf("made %d requests, want a bounded scan", got)
	}
	// Whatever it collected must not contain duplicates from re-reading the same page.
	seen := map[string]bool{}
	for _, m := range members {
		if seen[m] {
			t.Errorf("member %q returned more than once; the repeated page was not stopped", m)
		}
		seen[m] = true
	}
}

// FR-8 / FR-10: a channel that is not there is a broken reference, and the caller has
// to be able to tell it apart from an empty channel.
func TestListChannelMembers_ChannelNotFoundIsAnError(t *testing.T) {
	c, _ := newTestClient(t, routes{
		"/api/conversations.members": fixture("err_channel_not_found.json"),
	})

	members, err := c.ListChannelMembers("C000000000")
	if err == nil {
		t.Fatalf("expected an error for an unknown channel, got members=%v", members)
	}
	if members != nil {
		t.Errorf("members = %v, want nil on error", members)
	}
	if got := ErrorCode(err); got != "channel_not_found" {
		t.Errorf("ErrorCode = %q, want channel_not_found", got)
	}
}

func TestListChannelMembers_OkFalseSurfacesError(t *testing.T) {
	c, _ := newTestClient(t, routes{
		"/api/conversations.members": fixture("err_missing_scope.json"),
	})

	if _, err := c.ListChannelMembers("C012AB3CD"); ErrorCode(err) != "missing_scope" {
		t.Errorf("ErrorCode = %q, want missing_scope", ErrorCode(err))
	}
}

// An empty channel is a legitimate answer, not an error -- FR-7 decides what to do
// about emptiness, and it decides that one layer up.
func TestListChannelMembers_EmptyChannelIsNotAnError(t *testing.T) {
	c, _ := newTestClient(t, routes{
		"/api/conversations.members": raw(200, `{"ok":true,"members":[],"response_metadata":{"next_cursor":""}}`),
	})

	members, err := c.ListChannelMembers("C012AB3CD")
	if err != nil {
		t.Fatalf("an empty channel must not be an error: %v", err)
	}
	if len(members) != 0 {
		t.Errorf("members = %v, want empty", members)
	}
}
