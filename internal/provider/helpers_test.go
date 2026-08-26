package provider

// Test harness for the provider package: a stub Slack server plus builders for the
// tfsdk plumbing needed to drive resource CRUD methods directly.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"terraform-provider-slack/internal/slackclient"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

type stub struct {
	status  int
	fixture string
	body    string
}

func fixture(name string) stub { return stub{status: http.StatusOK, fixture: name} }

func raw(status int, body string) stub { return stub{status: status, body: body} }

// recordedCall is one request the stub server received. Only the fields the provider
// tests assert on are kept -- which endpoint was called, and with what parameters.
type recordedCall struct {
	Path  string
	Query url.Values
}

// callRecorder collects requests for assertion. Safe for concurrent handler goroutines.
type callRecorder struct {
	mu    sync.Mutex
	calls []recordedCall
}

func (c *callRecorder) add(v recordedCall) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, v)
}

// paths returns the endpoint path of every recorded call, in arrival order. Order is
// the point for reposting: the delete must land before the new message is posted.
func (c *callRecorder) paths() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, 0, len(c.calls))
	for _, call := range c.calls {
		out = append(out, call.Path)
	}
	return out
}

// callsTo returns every recorded call to the given endpoint path, in arrival order.
func (c *callRecorder) callsTo(path string) []recordedCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []recordedCall
	for _, call := range c.calls {
		if call.Path == path {
			out = append(out, call)
		}
	}
	return out
}

// newStubClient starts a stub Slack server and returns a client pointed at it.
// Fixtures are read from the slackclient package's testdata directory so both packages
// assert against the same recorded responses.
func newStubClient(t *testing.T, rt map[string]stub) *slackclient.Client {
	t.Helper()
	c, _ := newRecordingStubClient(t, rt)
	return c
}

// newRecordingStubClient is newStubClient plus a recorder, for tests that must assert
// on how the provider called Slack rather than only on what it did with the answer.
func newRecordingStubClient(t *testing.T, rt map[string]stub) (*slackclient.Client, *callRecorder) {
	t.Helper()

	rec := &callRecorder{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		rec.add(recordedCall{Path: req.URL.Path, Query: req.URL.Query()})

		s, ok := rt[req.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"ok":false,"error":"unknown_method"}`))
			return
		}
		payload := []byte(s.body)
		if s.fixture != "" {
			b, err := os.ReadFile(filepath.Join("..", "slackclient", "testdata", s.fixture))
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			payload = b
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(s.status)
		_, _ = w.Write(payload)
	}))
	t.Cleanup(srv.Close)

	host := srv.URL
	token := "xoxb-test-token"
	c, err := slackclient.NewClient(&host, &token)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c, rec
}

// msgMapObjectType is the tftypes shape of one msg_map entry.
var msgMapObjectType = tftypes.Object{
	AttributeTypes: map[string]tftypes.Type{
		"ts":      tftypes.String,
		"channel": tftypes.String,
	},
}

// msgEntry is one msg_map entry: what Slack returned when the message was posted.
// The distinction between the map key and Channel matters -- the key is the Slack ID
// the config named (a user ID for a DM), while Channel is the conversation Slack
// actually delivered to (a D... channel for that DM).
type msgEntry struct {
	ts      string
	channel string
}

func messageSchema(t *testing.T, r *messageResource) tfsdk.State {
	t.Helper()
	schemaResp := &resource.SchemaResponse{}
	r.Schema(context.Background(), resource.SchemaRequest{}, schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("building schema: %v", schemaResp.Diagnostics)
	}
	return tfsdk.State{Schema: schemaResp.Schema}
}

func slackIDSet(ids []string) tftypes.Value {
	vals := make([]tftypes.Value, 0, len(ids))
	for _, id := range ids {
		vals = append(vals, tftypes.NewValue(tftypes.String, id))
	}
	return tftypes.NewValue(tftypes.Set{ElementType: tftypes.String}, vals)
}

// messageStateOf builds a tfsdk.State for messageResource with arbitrary msg_map
// entries, so a test can distinguish the Slack ID key from the stored channel.
func messageStateOf(t *testing.T, r *messageResource, message string, slackIDs []string, entries map[string]msgEntry) tfsdk.State {
	t.Helper()
	ctx := context.Background()
	sch := messageSchema(t, r).Schema

	mapVals := make(map[string]tftypes.Value, len(entries))
	for id, e := range entries {
		mapVals[id] = tftypes.NewValue(msgMapObjectType, map[string]tftypes.Value{
			"ts":      tftypes.NewValue(tftypes.String, e.ts),
			"channel": tftypes.NewValue(tftypes.String, e.channel),
		})
	}

	raw := tftypes.NewValue(sch.Type().TerraformType(ctx), map[string]tftypes.Value{
		"message":      tftypes.NewValue(tftypes.String, message),
		"slack_ids":    slackIDSet(slackIDs),
		"last_updated": tftypes.NewValue(tftypes.String, "Monday, 25-Aug-26 20:09:32 UTC"),
		"msg_map":      tftypes.NewValue(tftypes.Map{ElementType: msgMapObjectType}, mapVals),
	})

	return tfsdk.State{Schema: sch, Raw: raw}
}

// messagePlanOf builds the tfsdk.Plan Terraform would hand Update: the configured
// values are known, the computed ones are not yet.
func messagePlanOf(t *testing.T, r *messageResource, message string, slackIDs []string) tfsdk.Plan {
	t.Helper()
	ctx := context.Background()
	sch := messageSchema(t, r).Schema

	raw := tftypes.NewValue(sch.Type().TerraformType(ctx), map[string]tftypes.Value{
		"message":      tftypes.NewValue(tftypes.String, message),
		"slack_ids":    slackIDSet(slackIDs),
		"last_updated": tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		"msg_map":      tftypes.NewValue(tftypes.Map{ElementType: msgMapObjectType}, tftypes.UnknownValue),
	})

	return tfsdk.Plan{Schema: sch, Raw: raw}
}

// messageStateWith builds a tfsdk.State for messageResource holding one msg_map entry.
func messageStateWith(t *testing.T, r *messageResource, slackID, channel, ts string) tfsdk.State {
	t.Helper()
	return messageStateOf(t, r,
		"Here's a message for you",
		[]string{slackID},
		map[string]msgEntry{slackID: {ts: ts, channel: channel}},
	)
}
