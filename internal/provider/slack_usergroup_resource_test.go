package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
)

func newUserGroupResource(t *testing.T, rt map[string]stub) *userGroupResource {
	t.Helper()
	return &userGroupResource{client: newStubClient(t, rt)}
}

func userGroupSchema(t *testing.T) resource.SchemaResponse {
	t.Helper()
	r := &userGroupResource{}
	resp := resource.SchemaResponse{}
	r.Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema: %v", resp.Diagnostics)
	}
	return resp
}

func TestUserGroupResource_MetadataTypeName(t *testing.T) {
	r := &userGroupResource{}
	resp := &resource.MetadataResponse{}
	r.Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "slack"}, resp)

	if resp.TypeName != "slack_usergroup" {
		t.Errorf("TypeName = %q, want slack_usergroup", resp.TypeName)
	}
}

// The developer asked for this explicitly: user groups are a paid-plan Slack feature and
// the docs must say so. tfplugindocs renders the schema Description verbatim, so asserting
// on it here is asserting on the published documentation.
func TestUserGroupResource_SchemaDocumentsPaidPlanRequirement(t *testing.T) {
	desc := userGroupSchema(t).Schema.Description

	lower := strings.ToLower(desc)
	if !strings.Contains(lower, "paid") {
		t.Errorf("schema description must state that user groups require a paid Slack plan; got: %s", desc)
	}
	if !strings.Contains(lower, "free") {
		t.Errorf("schema description should name the failure on free plans; got: %s", desc)
	}
	if !strings.Contains(desc, "paid_only") {
		t.Errorf("schema description should name the paid_only error code; got: %s", desc)
	}
}

// Slack has no delete. If the docs do not say so, it will be reported as a bug.
func TestUserGroupResource_SchemaDocumentsDisableNotDelete(t *testing.T) {
	desc := strings.ToLower(userGroupSchema(t).Schema.Description)

	if !strings.Contains(desc, "disable") {
		t.Errorf("schema description must explain that destroy disables rather than deletes; got: %s", desc)
	}
	if !strings.Contains(desc, "reserved") {
		t.Errorf("schema description must warn that the handle stays reserved; got: %s", desc)
	}
}

// Authoritative membership is the highest-scored risk in the spec; the attribute itself
// must carry the warning.
func TestUserGroupResource_UsersAttributeDocumentsAuthority(t *testing.T) {
	attrs := userGroupSchema(t).Schema.Attributes

	users, ok := attrs["users"]
	if !ok {
		t.Fatal("schema has no `users` attribute")
	}
	d := strings.ToLower(users.GetDescription())
	if !strings.Contains(d, "authoritative") {
		t.Errorf("users description must say it is authoritative; got: %s", users.GetDescription())
	}
	if !strings.Contains(d, "omit") {
		t.Errorf("users description must document omitting it as the escape hatch (FR-8a); got: %s", users.GetDescription())
	}
}

func TestUserGroupResource_SchemaHasExpectedAttributes(t *testing.T) {
	attrs := userGroupSchema(t).Schema.Attributes

	for _, name := range []string{
		"id", "name", "handle", "description", "channels", "users",
		"team_id", "user_count", "date_create", "date_update", "is_disabled",
		"is_idp_group", "is_membership_locked",
	} {
		if _, ok := attrs[name]; !ok {
			t.Errorf("schema is missing attribute %q", name)
		}
	}
}

func TestUserGroupResource_ConfigureRejectsWrongType(t *testing.T) {
	r := &userGroupResource{}
	resp := &resource.ConfigureResponse{}
	r.Configure(context.Background(), resource.ConfigureRequest{ProviderData: &notAClient{}}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("wrong ProviderData type must produce a diagnostic")
	}
	if !strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "*slackclient.Client") {
		t.Errorf("detail should name the expected type; got %s", resp.Diagnostics.Errors()[0].Detail())
	}
}

// AC-7 / AC-8: Slack's raw error codes tell an operator nothing actionable.
func TestUserGroupDiagnostic_PaidOnlyExplainsThePlanRequirement(t *testing.T) {
	summary, detail := userGroupErrorDiagnostic(&testSlackErr{code: "paid_only"}, "create", "eng")

	if !strings.Contains(strings.ToLower(summary+detail), "paid") {
		t.Errorf("paid_only diagnostic must explain the plan requirement; got %s / %s", summary, detail)
	}
}

func TestUserGroupDiagnostic_PermissionDeniedMentionsWorkspaceRestriction(t *testing.T) {
	_, detail := userGroupErrorDiagnostic(&testSlackErr{code: "permission_denied"}, "create", "eng")

	lower := strings.ToLower(detail)
	if !strings.Contains(lower, "workspace") && !strings.Contains(lower, "admin") {
		t.Errorf("permission_denied should point at the workspace restriction, not just repeat Slack; got: %s", detail)
	}
}

func TestUserGroupDiagnostic_HandleConflictSuggestsImport(t *testing.T) {
	_, detail := userGroupErrorDiagnostic(&testSlackErr{code: "handle_already_exists"}, "create", "eng")

	if !strings.Contains(strings.ToLower(detail), "import") {
		t.Errorf("a handle conflict should suggest terraform import; got: %s", detail)
	}
}
