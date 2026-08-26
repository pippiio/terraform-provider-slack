package provider

// Story: slack_message Delete
//
// Destroy removes every posted message. It has the same channel-targeting defect the
// Update path had: the msg_map key is the Slack ID the config named, not the
// conversation the message lives in.

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
)

func TestMessageResourceDelete_DeletesStoredChannel(t *testing.T) {
	ctx := context.Background()
	client, rec := newRecordingStubClient(t, map[string]stub{
		pathDelete: raw(200, `{"ok":true}`),
	})
	r := &messageResource{client: client}

	state := messageStateOf(t, r, "hello", []string{"U111"}, map[string]msgEntry{
		"U111": {ts: "1111.0001", channel: "D111"},
	})

	resp := &resource.DeleteResponse{State: state}
	r.Delete(ctx, resource.DeleteRequest{State: state}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	calls := rec.callsTo(pathDelete)
	if len(calls) != 1 {
		t.Fatalf("chat.delete called %d times, want 1", len(calls))
	}
	if got := calls[0].Query.Get("channel"); got != "D111" {
		t.Errorf("chat.delete channel = %q, want the stored channel %q", got, "D111")
	}
}

// Destroy must be able to finish when a message is already gone -- otherwise a
// message deleted by hand in Slack leaves a resource that can never be destroyed.
func TestMessageResourceDelete_AlreadyDeletedMessageIsTolerated(t *testing.T) {
	ctx := context.Background()
	r := &messageResource{client: newStubClient(t, map[string]stub{
		pathDelete: fixture("err_message_not_found.json"),
	})}

	state := messageStateOf(t, r, "hello", []string{"U111"}, map[string]msgEntry{
		"U111": {ts: "1111.0001", channel: "D111"},
	})

	resp := &resource.DeleteResponse{State: state}
	r.Delete(ctx, resource.DeleteRequest{State: state}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("an already-deleted message must not block destroy, got: %v", resp.Diagnostics)
	}
}

// A delete Slack actively refused must still fail loudly.
func TestMessageResourceDelete_FailureIsSurfaced(t *testing.T) {
	ctx := context.Background()
	r := &messageResource{client: newStubClient(t, map[string]stub{
		pathDelete: fixture("err_invalid_auth.json"),
	})}

	state := messageStateOf(t, r, "hello", []string{"U111"}, map[string]msgEntry{
		"U111": {ts: "1111.0001", channel: "D111"},
	})

	resp := &resource.DeleteResponse{State: state}
	r.Delete(ctx, resource.DeleteRequest{State: state}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("a refused chat.delete must produce a diagnostic")
	}
}
