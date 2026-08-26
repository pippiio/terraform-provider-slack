package provider

// Story: slack_message Update
//
// Update reconciles a posted message set against the plan: Slack IDs that left the
// plan have their messages deleted, new ones get a message posted, and a changed
// `message` is edited in place. Every one of those is a write against Slack that can
// fail, and before these tests none of them were covered.

import (
	"strings"
	"testing"

	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	pathDelete = "/api/chat.delete"
	pathUpdate = "/api/chat.update"
	pathPost   = "/api/chat.postMessage"
)

// A failed delete must not be silent. Update discarded DeleteMessage's error with
// `_ =`, so a delete that Slack refused still dropped the entry from state: the
// message stays in the workspace forever and Terraform no longer knows it exists.
func TestMessageResourceUpdate_DeleteFailureIsSurfaced(t *testing.T) {
	ctx := context.Background()
	r := &messageResource{client: newStubClient(t, map[string]stub{
		pathDelete: fixture("err_invalid_auth.json"),
	})}

	state := messageStateOf(t, r, "hello", []string{"U111", "U222"}, map[string]msgEntry{
		"U111": {ts: "1111.0001", channel: "D111"},
		"U222": {ts: "2222.0002", channel: "D222"},
	})
	plan := messagePlanOf(t, r, "hello", []string{"U111"})

	resp := &resource.UpdateResponse{State: state}
	r.Update(ctx, resource.UpdateRequest{Plan: plan, State: state}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("a refused chat.delete must produce a diagnostic, not be discarded")
	}
	if summary := resp.Diagnostics.Errors()[0].Summary(); !strings.Contains(summary, "Delet") {
		t.Errorf("diagnostic summary = %q, want it to name the failed delete", summary)
	}
}

// The msg_map key is the Slack ID from config -- for a DM that is a user ID (U...).
// The conversation Slack actually posted to is the stored `channel` (D...). Deleting
// with the key targets a channel that does not exist.
func TestMessageResourceUpdate_RemovalDeletesStoredChannel(t *testing.T) {
	ctx := context.Background()
	client, rec := newRecordingStubClient(t, map[string]stub{
		pathDelete: raw(200, `{"ok":true}`),
	})
	r := &messageResource{client: client}

	state := messageStateOf(t, r, "hello", []string{"U111", "U222"}, map[string]msgEntry{
		"U111": {ts: "1111.0001", channel: "D111"},
		"U222": {ts: "2222.0002", channel: "D222"},
	})
	plan := messagePlanOf(t, r, "hello", []string{"U111"})

	resp := &resource.UpdateResponse{State: state}
	r.Update(ctx, resource.UpdateRequest{Plan: plan, State: state}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	calls := rec.callsTo(pathDelete)
	if len(calls) != 1 {
		t.Fatalf("chat.delete called %d times, want 1", len(calls))
	}
	if got := calls[0].Query.Get("channel"); got != "D222" {
		t.Errorf("chat.delete channel = %q, want the stored channel %q", got, "D222")
	}
	if got := calls[0].Query.Get("ts"); got != "2222.0002" {
		t.Errorf("chat.delete ts = %q, want %q", got, "2222.0002")
	}
}

// Same defect on the edit path: chat.update must target the stored channel.
func TestMessageResourceUpdate_EditUsesStoredChannel(t *testing.T) {
	ctx := context.Background()
	client, rec := newRecordingStubClient(t, map[string]stub{
		pathUpdate: fixture("chat_postmessage_ok.json"),
	})
	r := &messageResource{client: client}

	state := messageStateOf(t, r, "old text", []string{"U111"}, map[string]msgEntry{
		"U111": {ts: "1111.0001", channel: "D111"},
	})
	plan := messagePlanOf(t, r, "new text", []string{"U111"})

	resp := &resource.UpdateResponse{State: state}
	r.Update(ctx, resource.UpdateRequest{Plan: plan, State: state}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	calls := rec.callsTo(pathUpdate)
	if len(calls) != 1 {
		t.Fatalf("chat.update called %d times, want 1", len(calls))
	}
	if got := calls[0].Query.Get("channel"); got != "D111" {
		t.Errorf("chat.update channel = %q, want the stored channel %q", got, "D111")
	}
	if got := calls[0].Query.Get("text"); got != "new text" {
		t.Errorf("chat.update text = %q, want %q", got, "new text")
	}
}

// Characterization: a Slack ID added to the plan gets a message posted, and the ts
// Slack answers with is recorded in state.
func TestMessageResourceUpdate_NewSlackIDIsMessaged(t *testing.T) {
	ctx := context.Background()
	client, rec := newRecordingStubClient(t, map[string]stub{
		pathPost: fixture("chat_postmessage_ok.json"),
	})
	r := &messageResource{client: client}

	state := messageStateOf(t, r, "hello", []string{"U111"}, map[string]msgEntry{
		"U111": {ts: "1111.0001", channel: "D111"},
	})
	plan := messagePlanOf(t, r, "hello", []string{"U111", "U222"})

	resp := &resource.UpdateResponse{State: state}
	r.Update(ctx, resource.UpdateRequest{Plan: plan, State: state}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	if calls := rec.callsTo(pathPost); len(calls) != 1 {
		t.Fatalf("chat.postMessage called %d times, want 1", len(calls))
	}

	var out messageResourceModel
	if diags := resp.State.Get(ctx, &out); diags.HasError() {
		t.Fatalf("reading result state: %v", diags)
	}
	entries := out.Msg_map.Elements()
	if len(entries) != 2 {
		t.Fatalf("msg_map has %d entries, want 2", len(entries))
	}
	added, ok := entries["U222"]
	if !ok {
		t.Fatal("msg_map has no entry for the newly added Slack ID")
	}
	if ts := added.(types.Object).Attributes()["ts"].(types.String).ValueString(); ts != "1503435956.000247" {
		t.Errorf("new entry ts = %q, want the ts Slack answered with", ts)
	}
}

// A failed post must fail the apply rather than writing a half-built msg_map.
func TestMessageResourceUpdate_PostFailureIsSurfaced(t *testing.T) {
	ctx := context.Background()
	r := &messageResource{client: newStubClient(t, map[string]stub{
		pathPost: fixture("err_missing_scope.json"),
	})}

	state := messageStateOf(t, r, "hello", []string{"U111"}, map[string]msgEntry{
		"U111": {ts: "1111.0001", channel: "D111"},
	})
	plan := messagePlanOf(t, r, "hello", []string{"U111", "U222"})

	resp := &resource.UpdateResponse{State: state}
	r.Update(ctx, resource.UpdateRequest{Plan: plan, State: state}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("a refused chat.postMessage must produce a diagnostic")
	}
}

// A message somebody already deleted in Slack must not wedge the apply. The desired
// end state -- the message gone -- is already true, so removing the Slack ID from the
// plan should succeed rather than failing forever on message_not_found.
func TestMessageResourceUpdate_AlreadyDeletedMessageIsTolerated(t *testing.T) {
	ctx := context.Background()
	r := &messageResource{client: newStubClient(t, map[string]stub{
		pathDelete: fixture("err_message_not_found.json"),
	})}

	state := messageStateOf(t, r, "hello", []string{"U111", "U222"}, map[string]msgEntry{
		"U111": {ts: "1111.0001", channel: "D111"},
		"U222": {ts: "2222.0002", channel: "D222"},
	})
	plan := messagePlanOf(t, r, "hello", []string{"U111"})

	resp := &resource.UpdateResponse{State: state}
	r.Update(ctx, resource.UpdateRequest{Plan: plan, State: state}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("an already-deleted message is the desired end state, got: %v", resp.Diagnostics)
	}

	var out messageResourceModel
	if diags := resp.State.Get(ctx, &out); diags.HasError() {
		t.Fatalf("reading result state: %v", diags)
	}
	if _, still := out.Msg_map.Elements()["U222"]; still {
		t.Error("the removed Slack ID is still in msg_map")
	}
}
