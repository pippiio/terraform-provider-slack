package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// userGroupPlan builds a tfsdk plan with everything null except the fields given.
// Deriving nulls from the schema keeps this from needing an edit per new attribute.
func userGroupPlan(t *testing.T, r *userGroupResource, name, handle string, users []string) tfsdk.Plan {
	t.Helper()
	ctx := context.Background()

	sr := resource.SchemaResponse{}
	r.Schema(ctx, resource.SchemaRequest{}, &sr)
	sch := sr.Schema
	objType := sch.Type().TerraformType(ctx).(tftypes.Object)

	vals := make(map[string]tftypes.Value, len(objType.AttributeTypes))
	for n, typ := range objType.AttributeTypes {
		vals[n] = tftypes.NewValue(typ, nil)
	}
	vals["name"] = tftypes.NewValue(tftypes.String, name)
	vals["handle"] = tftypes.NewValue(tftypes.String, handle)
	if users != nil {
		elems := make([]tftypes.Value, 0, len(users))
		for _, u := range users {
			elems = append(elems, tftypes.NewValue(tftypes.String, u))
		}
		vals["users"] = tftypes.NewValue(tftypes.Set{ElementType: tftypes.String}, elems)
	}
	return tfsdk.Plan{Schema: sch, Raw: tftypes.NewValue(objType, vals)}
}

func createUserGroup(t *testing.T, r *userGroupResource, plan tfsdk.Plan) *resource.CreateResponse {
	t.Helper()
	ctx := context.Background()
	objType := plan.Schema.Type().TerraformType(ctx).(tftypes.Object)
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: plan.Schema, Raw: tftypes.NewValue(objType, nil)}}
	r.Create(ctx, resource.CreateRequest{Plan: plan}, resp)
	return resp
}

const emptyList = `{"ok":true,"usergroups":[]}`

// AC-1a / AC-1b: a plain create when nothing owns the handle.
func TestUserGroupCreate_NoConflictCreates(t *testing.T) {
	r := newUserGroupResource(t, map[string]stub{
		"/api/usergroups.list":   raw(200, emptyList),
		"/api/usergroups.create": fixture("usergroups_create_ok.json"),
	})
	resp := createUserGroup(t, r, userGroupPlan(t, r, "Marketing Team", "marketing-team", nil))

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	var m userGroupResourceModel
	if d := resp.State.Get(context.Background(), &m); d.HasError() {
		t.Fatalf("reading state: %v", d)
	}
	if m.ID.ValueString() != "S0615G0KT" {
		t.Errorf("id = %q, want S0615G0KT", m.ID.ValueString())
	}
	if m.IsDisabled.ValueBool() {
		t.Error("newly created group reported as disabled")
	}
}

// AC-1e / AC-1f: Slack keeps handles reserved after disable, so re-creating must adopt —
// and must say so, because it revives a group somebody deliberately disabled.
func TestUserGroupCreate_AdoptsDisabledGroupAndWarns(t *testing.T) {
	r := newUserGroupResource(t, map[string]stub{
		"/api/usergroups.list":   fixture("usergroups_list_recorded.json"),
		"/api/usergroups.enable": fixture("usergroups_create_ok.json"),
		"/api/usergroups.update": fixture("usergroups_create_ok.json"),
	})
	// "example-retired" is the disabled group in the recorded fixture.
	resp := createUserGroup(t, r, userGroupPlan(t, r, "Revived Team", "example-retired", nil))

	if resp.Diagnostics.HasError() {
		t.Fatalf("adopting a disabled group must succeed: %v", resp.Diagnostics)
	}
	if resp.Diagnostics.WarningsCount() == 0 {
		t.Fatal("adoption must emit a warning -- it revives a deliberately disabled group")
	}
	w := resp.Diagnostics.Warnings()[0]
	if !strings.Contains(w.Detail(), "S06MMAAAA") {
		t.Errorf("warning should name the adopted group ID; got: %s", w.Detail())
	}
}

// AC-1d: an active group with the same handle belongs to somebody else. Taking it over
// silently would be a far worse failure than erroring.
func TestUserGroupCreate_ActiveHandleConflictFailsAndSuggestsImport(t *testing.T) {
	r := newUserGroupResource(t, map[string]stub{
		"/api/usergroups.list": fixture("usergroups_list_recorded.json"),
	})
	// "example-alpha" is the active group in the recorded fixture.
	resp := createUserGroup(t, r, userGroupPlan(t, r, "Hijack Attempt", "example-alpha", nil))

	if !resp.Diagnostics.HasError() {
		t.Fatal("creating against an active handle must fail, not silently adopt")
	}
	detail := resp.Diagnostics.Errors()[0].Detail()
	if !strings.Contains(detail, "terraform import") {
		t.Errorf("error should suggest terraform import; got: %s", detail)
	}
	if !strings.Contains(detail, "S0615G0KT") {
		t.Errorf("error should name the conflicting group ID; got: %s", detail)
	}
}

// AC-2a: members are applied on create.
func TestUserGroupCreate_AppliesMembership(t *testing.T) {
	r := newUserGroupResource(t, map[string]stub{
		"/api/usergroups.list":         raw(200, emptyList),
		"/api/usergroups.create":       fixture("usergroups_create_ok.json"),
		"/api/usergroups.users.update": fixture("usergroups_users_update_ok.json"),
	})
	resp := createUserGroup(t, r, userGroupPlan(t, r, "Marketing Team", "marketing-team",
		[]string{"U060RNRCZ", "U060ULRC0"}))

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	var m userGroupResourceModel
	_ = resp.State.Get(context.Background(), &m)
	if len(m.Users.Elements()) != 2 {
		t.Errorf("users = %v, want 2 members", m.Users.Elements())
	}
}

// AC-2f / FR-8a: omitting `users` must leave membership alone. If the resource called
// users.update with an empty list here it would silently empty the group.
func TestUserGroupCreate_OmittedUsersMakesNoMembershipCall(t *testing.T) {
	r := newUserGroupResource(t, map[string]stub{
		"/api/usergroups.list":   raw(200, emptyList),
		"/api/usergroups.create": fixture("usergroups_create_ok.json"),
		// usergroups.users.update is deliberately NOT routed: reaching it 404s the stub,
		// which surfaces as an error and fails this test.
	})
	resp := createUserGroup(t, r, userGroupPlan(t, r, "Marketing Team", "marketing-team", nil))

	if resp.Diagnostics.HasError() {
		t.Fatalf("omitting users must not trigger a membership call: %v", resp.Diagnostics)
	}
	var m userGroupResourceModel
	_ = resp.State.Get(context.Background(), &m)
	if !m.Users.IsNull() {
		t.Errorf("users = %v, want null when omitted -- populating it would start managing membership", m.Users)
	}
}

// AC-2g: membership on an IdP-synced or locked group is not ours to manage.
func TestUserGroupCreate_RefusesMembershipOnIDPGroup(t *testing.T) {
	r := newUserGroupResource(t, map[string]stub{
		"/api/usergroups.list":   fixture("usergroups_list_recorded.json"),
		"/api/usergroups.enable": fixture("usergroups_create_ok.json"),
		"/api/usergroups.update": raw(200, `{"ok":true,"usergroup":{"id":"S07IDPAAA","name":"Example SSO Team","handle":"example-sso","date_delete":0,"is_idp_group":true,"is_membership_locked":true}}`),
	})
	// example-sso is active in the fixture, so this exercises the conflict path first;
	// use the disabled handle instead to reach membership with an IdP group returned.
	plan := userGroupPlan(t, r, "SSO Team", "example-retired", []string{"U060RNRCZ"})
	resp := createUserGroup(t, r, plan)

	if !resp.Diagnostics.HasError() {
		t.Fatal("setting users on an IdP-synced group must fail")
	}
	detail := resp.Diagnostics.Errors()[0].Detail()
	if !strings.Contains(strings.ToLower(detail), "identity provider") {
		t.Errorf("diagnostic should explain the IdP ownership; got: %s", detail)
	}
}

// AC-4: destroy disables. Routing only usergroups.disable proves nothing else is called.
func TestUserGroupDelete_DisablesRatherThanDeletes(t *testing.T) {
	ctx := context.Background()
	r := newUserGroupResource(t, map[string]stub{
		"/api/usergroups.disable": fixture("usergroups_disable_ok.json"),
	})
	plan := userGroupPlan(t, r, "Marketing Team", "marketing-team", nil)
	state := tfsdk.State{Schema: plan.Schema, Raw: plan.Raw}

	resp := &resource.DeleteResponse{State: state}
	r.Delete(ctx, resource.DeleteRequest{State: state}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("delete should succeed via usergroups.disable: %v", resp.Diagnostics)
	}
}

// Review finding: on the adopt path the group handed to applyMembership comes from
// usergroups.update, whose response may omit is_idp_group / is_membership_locked. The
// flags we can trust come from usergroups.list. If the thin response wins, an IdP-synced
// group looks manageable and the provider writes members it should refuse to touch.
func TestUserGroupCreate_AdoptKeepsIDPFlagsFromListResponse(t *testing.T) {
	// A disabled group that is ALSO IdP-synced and membership-locked.
	listBody := `{"ok":true,"usergroups":[{
		"id":"S07LOCKED","name":"Locked Team","handle":"locked-team",
		"date_delete":1446746800,"is_idp_group":true,"is_membership_locked":true,
		"prefs":{"channels":[],"groups":[]},"users":[],"user_count":0}]}`

	// The update response is thin -- no flags, exactly the shape that hides the problem.
	thinUpdate := `{"ok":true,"usergroup":{"id":"S07LOCKED","name":"Locked Team","handle":"locked-team","date_delete":0}}`

	r := newUserGroupResource(t, map[string]stub{
		"/api/usergroups.list":   raw(200, listBody),
		"/api/usergroups.enable": raw(200, thinUpdate),
		"/api/usergroups.update": raw(200, thinUpdate),
		// usergroups.users.update deliberately unrouted: reaching it means we failed.
	})

	resp := createUserGroup(t, r, userGroupPlan(t, r, "Locked Team", "locked-team", []string{"U060RNRCZ"}))

	if !resp.Diagnostics.HasError() {
		t.Fatal("adopting an IdP-synced group and writing members must be refused")
	}
	detail := resp.Diagnostics.Errors()[0].Detail()
	if !strings.Contains(strings.ToLower(detail), "identity provider") {
		t.Errorf("diagnostic should name the IdP ownership, not a generic Slack error; got: %s", detail)
	}
}
