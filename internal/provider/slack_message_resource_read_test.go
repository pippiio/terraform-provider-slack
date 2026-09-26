package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// AC-5: drift detection is preserved. A message deleted in Slack answers
// thread_not_found; the entry is dropped, and with no entries left the resource is
// removed from state so Terraform recreates it.
func TestMessageResourceRead_ThreadNotFoundRemovesResource(t *testing.T) {
	ctx := context.Background()
	r := &messageResource{client: newStubClient(t, map[string]stub{
		"/api/conversations.replies": fixture("err_thread_not_found.json"),
	})}

	state := messageStateWith(t, r, "C123456789", "C123456789", "1503435956.000247")
	resp := &resource.ReadResponse{State: state}
	r.Read(ctx, resource.ReadRequest{State: state}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("thread_not_found should not error, got: %v", resp.Diagnostics)
	}
	if !resp.State.Raw.IsNull() {
		t.Error("expected the resource to be removed from state when every entry is gone")
	}
}

// AC-6 / A-4: a failure to reach Slack must NOT be treated as deletion. Before this
// fix, `if err != nil || ...` dropped the entry, silently destroying state for a
// message that still exists and causing Terraform to post a duplicate.
func TestMessageResourceRead_TransportFailureDoesNotDropState(t *testing.T) {
	ctx := context.Background()
	r := &messageResource{client: newStubClient(t, map[string]stub{
		"/api/conversations.replies": raw(500, `{"ok":false,"error":"internal_error"}`),
	})}

	state := messageStateWith(t, r, "C123456789", "C123456789", "1503435956.000247")
	resp := &resource.ReadResponse{State: state}
	r.Read(ctx, resource.ReadRequest{State: state}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("a transport failure must produce a diagnostic, not a silent state drop")
	}
	if resp.State.Raw.IsNull() {
		t.Error("state was removed on a transport failure -- this is the A-4 data-loss path")
	}
	if summary := resp.Diagnostics.Errors()[0].Summary(); !strings.Contains(summary, "Unable to read Slack message") {
		t.Errorf("diagnostic summary = %q, want it to name the read failure", summary)
	}
}

// An auth failure is likewise not evidence that the message is gone.
func TestMessageResourceRead_AuthFailureDoesNotDropState(t *testing.T) {
	ctx := context.Background()
	r := &messageResource{client: newStubClient(t, map[string]stub{
		"/api/conversations.replies": fixture("err_invalid_auth.json"),
	})}

	state := messageStateWith(t, r, "C123456789", "C123456789", "1503435956.000247")
	resp := &resource.ReadResponse{State: state}
	r.Read(ctx, resource.ReadRequest{State: state}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("invalid_auth must produce a diagnostic")
	}
	if resp.State.Raw.IsNull() {
		t.Error("state was removed on an auth failure -- must not be treated as deletion")
	}
}

// The happy path still refreshes ts/channel from the response.
func TestMessageResourceRead_SuccessKeepsEntry(t *testing.T) {
	ctx := context.Background()
	r := &messageResource{client: newStubClient(t, map[string]stub{
		"/api/conversations.replies": fixture("conversations_replies_ok.json"),
	})}

	state := messageStateWith(t, r, "C123456789", "C123456789", "1503435956.000247")
	resp := &resource.ReadResponse{State: state}
	r.Read(ctx, resource.ReadRequest{State: state}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	if resp.State.Raw.IsNull() {
		t.Fatal("resource was removed from state despite a successful read")
	}
}
