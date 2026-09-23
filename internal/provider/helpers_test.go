package provider

// Test harness for the provider package: a stub Slack server plus builders for the
// tfsdk plumbing needed to drive resource CRUD methods directly.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

// newStubClient starts a stub Slack server and returns a client pointed at it.
// Fixtures are read from the slackclient package's testdata directory so both packages
// assert against the same recorded responses.
func newStubClient(t *testing.T, rt map[string]stub) *slackclient.Client {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
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
	return c
}

// msgMapObjectType is the tftypes shape of one msg_map entry.
var msgMapObjectType = tftypes.Object{
	AttributeTypes: map[string]tftypes.Type{
		"ts":      tftypes.String,
		"channel": tftypes.String,
	},
}

// messageStateWith builds a tfsdk.State for messageResource holding one msg_map entry.
func messageStateWith(t *testing.T, r *messageResource, slackID, channel, ts string) tfsdk.State {
	t.Helper()
	ctx := context.Background()

	schemaResp := &resource.SchemaResponse{}
	r.Schema(ctx, resource.SchemaRequest{}, schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("building schema: %v", schemaResp.Diagnostics)
	}
	sch := schemaResp.Schema

	raw := tftypes.NewValue(sch.Type().TerraformType(ctx), map[string]tftypes.Value{
		"message": tftypes.NewValue(tftypes.String, "Here's a message for you"),
		"slack_ids": tftypes.NewValue(
			tftypes.Set{ElementType: tftypes.String},
			[]tftypes.Value{tftypes.NewValue(tftypes.String, slackID)},
		),
		"last_updated": tftypes.NewValue(tftypes.String, "Monday, 25-Aug-26 20:09:32 UTC"),
		"msg_map": tftypes.NewValue(
			tftypes.Map{ElementType: msgMapObjectType},
			map[string]tftypes.Value{
				slackID: tftypes.NewValue(msgMapObjectType, map[string]tftypes.Value{
					"ts":      tftypes.NewValue(tftypes.String, ts),
					"channel": tftypes.NewValue(tftypes.String, channel),
				}),
			},
		),
	})

	return tfsdk.State{Schema: sch, Raw: raw}
}
