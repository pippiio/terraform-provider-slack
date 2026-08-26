package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func usergroupDSConfig(t *testing.T, d *userGroupDataSource, id, handle *string) tfsdk.Config {
	t.Helper()
	ctx := context.Background()

	sr := &datasource.SchemaResponse{}
	d.Schema(ctx, datasource.SchemaRequest{}, sr)
	if sr.Diagnostics.HasError() {
		t.Fatalf("schema: %v", sr.Diagnostics)
	}
	objType := sr.Schema.Type().TerraformType(ctx).(tftypes.Object)

	vals := make(map[string]tftypes.Value, len(objType.AttributeTypes))
	for n, typ := range objType.AttributeTypes {
		vals[n] = tftypes.NewValue(typ, nil)
	}
	if id != nil {
		vals["id"] = tftypes.NewValue(tftypes.String, *id)
	}
	if handle != nil {
		vals["handle"] = tftypes.NewValue(tftypes.String, *handle)
	}
	return tfsdk.Config{Schema: sr.Schema, Raw: tftypes.NewValue(objType, vals)}
}

func readUserGroupDS(t *testing.T, d *userGroupDataSource, cfg tfsdk.Config) *datasource.ReadResponse {
	t.Helper()
	ctx := context.Background()
	objType := cfg.Schema.Type().TerraformType(ctx).(tftypes.Object)
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: cfg.Schema, Raw: tftypes.NewValue(objType, nil)}}
	d.Read(ctx, datasource.ReadRequest{Config: cfg}, resp)
	return resp
}

func TestUserGroupDataSource_MetadataTypeName(t *testing.T) {
	d := &userGroupDataSource{}
	resp := &datasource.MetadataResponse{}
	d.Metadata(context.Background(), datasource.MetadataRequest{ProviderTypeName: "slack"}, resp)
	if resp.TypeName != "slack_usergroup" {
		t.Errorf("TypeName = %q, want slack_usergroup", resp.TypeName)
	}
}

// The developer asked for the paid-plan requirement to appear in resource documentation;
// the data source is equally unusable on a free plan, so it says so too.
func TestUserGroupDataSource_SchemaDocumentsPaidPlanRequirement(t *testing.T) {
	d := &userGroupDataSource{}
	sr := &datasource.SchemaResponse{}
	d.Schema(context.Background(), datasource.SchemaRequest{}, sr)

	if !strings.Contains(strings.ToLower(sr.Schema.Description), "paid") {
		t.Errorf("data source description must state the paid-plan requirement; got: %s", sr.Schema.Description)
	}
}

// AC-5: exactly one of id or handle, enforced before any API call.
func TestUserGroupDataSource_ExactlyOneOfIDOrHandle(t *testing.T) {
	d := &userGroupDataSource{}
	ctx := context.Background()

	check := func(id, handle *string) bool {
		resp := &datasource.ValidateConfigResponse{}
		for _, v := range d.ConfigValidators(ctx) {
			v.ValidateDataSource(ctx, datasource.ValidateConfigRequest{
				Config: usergroupDSConfig(t, d, id, handle),
			}, resp)
		}
		return resp.Diagnostics.HasError()
	}

	if !check(ptr("S1"), ptr("h")) {
		t.Error("both id and handle must be a config error")
	}
	if !check(nil, nil) {
		t.Error("neither id nor handle must be a config error")
	}
	if check(ptr("S0615G0KT"), nil) {
		t.Error("id alone must be valid")
	}
	if check(nil, ptr("example-alpha")) {
		t.Error("handle alone must be valid")
	}
}

func TestUserGroupDataSourceRead_ByHandle(t *testing.T) {
	d := &userGroupDataSource{client: newStubClient(t, map[string]stub{
		"/api/usergroups.list": fixture("usergroups_list_recorded.json"),
	})}
	resp := readUserGroupDS(t, d, usergroupDSConfig(t, d, nil, ptr("example-alpha")))

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	var m userGroupDataSourceModel
	if diags := resp.State.Get(context.Background(), &m); diags.HasError() {
		t.Fatalf("reading state: %v", diags)
	}
	if m.ID.ValueString() != "S0615G0KT" {
		t.Errorf("id = %q, want S0615G0KT", m.ID.ValueString())
	}
	if m.Name.ValueString() != "Example Alpha Team" {
		t.Errorf("name = %q", m.Name.ValueString())
	}
	if len(m.Users.Elements()) != 2 {
		t.Errorf("users = %v, want 2", m.Users.Elements())
	}
}

func TestUserGroupDataSourceRead_ByID(t *testing.T) {
	d := &userGroupDataSource{client: newStubClient(t, map[string]stub{
		"/api/usergroups.list": fixture("usergroups_list_recorded.json"),
	})}
	resp := readUserGroupDS(t, d, usergroupDSConfig(t, d, ptr("S07IDPAAA"), nil))

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	var m userGroupDataSourceModel
	_ = resp.State.Get(context.Background(), &m)
	if m.Handle.ValueString() != "example-sso" {
		t.Errorf("handle = %q, want example-sso", m.Handle.ValueString())
	}
	// Surfacing these lets config avoid managing membership it does not own.
	if !m.IsIDPGroup.ValueBool() {
		t.Error("is_idp_group should be true for the IdP-synced fixture group")
	}
	if !m.IsMembershipLocked.ValueBool() {
		t.Error("is_membership_locked should be true for the IdP-synced fixture group")
	}
}

func TestUserGroupDataSourceRead_NotFoundFails(t *testing.T) {
	d := &userGroupDataSource{client: newStubClient(t, map[string]stub{
		"/api/usergroups.list": fixture("usergroups_list_recorded.json"),
	})}
	resp := readUserGroupDS(t, d, usergroupDSConfig(t, d, nil, ptr("no-such-handle")))

	if !resp.Diagnostics.HasError() {
		t.Fatal("an unknown handle must fail rather than return an empty group")
	}
}

// Disabled groups are findable: their handles stay reserved, so config may need to see them.
func TestUserGroupDataSourceRead_FindsDisabledGroup(t *testing.T) {
	d := &userGroupDataSource{client: newStubClient(t, map[string]stub{
		"/api/usergroups.list": fixture("usergroups_list_recorded.json"),
	})}
	resp := readUserGroupDS(t, d, usergroupDSConfig(t, d, nil, ptr("example-retired")))

	if resp.Diagnostics.HasError() {
		t.Fatalf("a disabled group should still be readable: %v", resp.Diagnostics)
	}
	var m userGroupDataSourceModel
	_ = resp.State.Get(context.Background(), &m)
	if !m.IsDisabled.ValueBool() {
		t.Error("is_disabled should be true for the disabled fixture group")
	}
}
