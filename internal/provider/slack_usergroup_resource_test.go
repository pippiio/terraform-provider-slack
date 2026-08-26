package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
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

// Q1 from the Phase 1 probe: sending an empty `users` list produces Slack's
// invalid_arguments ("input must match regex ^[UW][A-Z0-9]{2,}$" on users/0) rather than
// a useful message. A plan-time validator turns that into an instruction.
//
// Note what this does NOT claim: Slack was never shown to forbid zero-member groups. It
// rejects an empty *string* as a user ID. The validator exists because the provider has
// no way to express "no members" through usergroups.users.update, not because Slack
// mandates a minimum.
func TestUserGroupResource_UsersRejectsEmptySet(t *testing.T) {
	attrs := userGroupSchema(t).Schema.Attributes

	users, ok := attrs["users"].(interface {
		SetValidators() []validator.Set
	})
	if !ok {
		t.Fatal("users attribute exposes no set validators")
	}
	if len(users.SetValidators()) == 0 {
		t.Fatal("users must carry a validator rejecting an empty set")
	}

	ctx := context.Background()
	empty, diags := types.SetValue(types.StringType, []attr.Value{})
	if diags.HasError() {
		t.Fatalf("building empty set: %v", diags)
	}

	resp := &validator.SetResponse{}
	users.SetValidators()[0].ValidateSet(ctx, validator.SetRequest{
		Path:        path.Root("users"),
		ConfigValue: empty,
	}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("an empty users set must be rejected at plan time")
	}
}

// A non-empty set must pass, and a null one must pass too -- omitting `users` is the
// supported way to leave membership to Slack (FR-8a).
func TestUserGroupResource_UsersAcceptsNonEmptyAndNull(t *testing.T) {
	attrs := userGroupSchema(t).Schema.Attributes
	users := attrs["users"].(interface {
		SetValidators() []validator.Set
	})
	ctx := context.Background()

	populated, _ := types.SetValue(types.StringType, []attr.Value{types.StringValue("U012ABCDE")})
	for name, v := range map[string]types.Set{
		"populated": populated,
		"null":      types.SetNull(types.StringType),
	} {
		t.Run(name, func(t *testing.T) {
			resp := &validator.SetResponse{}
			users.SetValidators()[0].ValidateSet(ctx, validator.SetRequest{
				Path:        path.Root("users"),
				ConfigValue: v,
			}, resp)
			if resp.Diagnostics.HasError() {
				t.Errorf("%s set must be accepted: %v", name, resp.Diagnostics)
			}
		})
	}
}
