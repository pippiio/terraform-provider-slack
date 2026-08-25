package provider

// AC-2b: automated field coverage between a recorded Slack response and the schema.
//
// A manual one-off diff rots the moment Slack adds a field. This walks a recorded
// response and asserts every key is either exposed in the schema or listed as a
// deliberate omission with a reason -- so a new field cannot be silently dropped.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	fwschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
)

// justifiedUserOmissions are top-level fields deliberately not exposed, per the track's
// Non-Goals. The value is the reason, kept here so the justification lives with the test.
var justifiedUserOmissions = map[string]string{
	"enterprise_user": "Enterprise Grid only; null for every other workspace. Add on request.",
	"locale":          "requires include_locale=true on the request, which this data source does not send.",
}

var justifiedProfileOmissions = map[string]string{
	"fields": "tenant-defined custom profile fields. Slack returns {} when populated and [] when empty, " +
		"so it needs a custom unmarshaller, and the arbitrary key/value shape does not map to a static " +
		"Terraform schema. Tracked as spec Open Question 1.",
}

func schemaAttributeNames(t *testing.T) (top map[string]bool, profile map[string]bool) {
	t.Helper()
	ctx := context.Background()

	d := &userDataSource{}
	resp := &datasource.SchemaResponse{}
	d.Schema(ctx, datasource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema: %v", resp.Diagnostics)
	}

	top = make(map[string]bool)
	profile = make(map[string]bool)
	for name, attribute := range resp.Schema.Attributes {
		top[name] = true
		if nested, ok := attribute.(fwschema.SingleNestedAttribute); ok && name == "profile" {
			for pname := range nested.Attributes {
				profile[pname] = true
			}
		}
	}
	return top, profile
}

func recordedUserObject(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "slackclient", "testdata", "users_info_with_excluded_fields.json"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	var envelope struct {
		User map[string]json.RawMessage `json:"user"`
	}
	if err := json.Unmarshal(b, &envelope); err != nil {
		t.Fatalf("unmarshalling fixture: %v", err)
	}
	return envelope.User
}

func TestUserSchema_CoversEveryRecordedTopLevelField(t *testing.T) {
	top, _ := schemaAttributeNames(t)

	for field := range recordedUserObject(t) {
		if field == "profile" {
			continue // covered by the nested test below
		}
		if top[field] {
			continue
		}
		if reason, justified := justifiedUserOmissions[field]; justified {
			t.Logf("omitted (justified): user.%s -- %s", field, reason)
			continue
		}
		t.Errorf("user.%s appears in the recorded Slack response but is neither in the schema "+
			"nor listed as a justified omission", field)
	}
}

func TestUserSchema_CoversEveryRecordedProfileField(t *testing.T) {
	_, profile := schemaAttributeNames(t)

	raw, ok := recordedUserObject(t)["profile"]
	if !ok {
		t.Fatal("fixture has no profile object")
	}
	var profileFields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &profileFields); err != nil {
		t.Fatalf("unmarshalling profile: %v", err)
	}

	for field := range profileFields {
		if profile[field] {
			continue
		}
		if reason, justified := justifiedProfileOmissions[field]; justified {
			t.Logf("omitted (justified): profile.%s -- %s", field, reason)
			continue
		}
		t.Errorf("profile.%s appears in the recorded Slack response but is neither in the schema "+
			"nor listed as a justified omission", field)
	}
}

// Guards the reverse direction: a schema attribute with no backing field would silently
// render null forever. `id` and `email` are exempt -- they are also config inputs.
func TestUserSchema_HasNoAttributesMissingFromTheResponse(t *testing.T) {
	top, profile := schemaAttributeNames(t)
	user := recordedUserObject(t)

	exempt := map[string]bool{"id": true, "email": true, "profile": true}
	for name := range top {
		if exempt[name] {
			continue
		}
		if _, ok := user[name]; !ok {
			t.Errorf("schema exposes %q but no such field appears in the recorded response", name)
		}
	}

	var profileFields map[string]json.RawMessage
	_ = json.Unmarshal(user["profile"], &profileFields)
	for name := range profile {
		if _, ok := profileFields[name]; !ok {
			t.Errorf("schema exposes profile.%q but no such field appears in the recorded response", name)
		}
	}
}
