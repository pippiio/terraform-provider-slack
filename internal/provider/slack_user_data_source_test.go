package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// userSchema returns the data source schema, failing the test on diagnostics.
func userSchema(t *testing.T, d *userDataSource) tfsdk.Config {
	t.Helper()
	return userConfig(t, d, nil, nil)
}

// userConfig builds a config with every attribute null except the ones supplied.
// Deriving the nulls from the schema keeps this from needing an update every time an
// attribute is added.
func userConfig(t *testing.T, d *userDataSource, id, email *string) tfsdk.Config {
	t.Helper()
	ctx := context.Background()

	schemaResp := &datasource.SchemaResponse{}
	d.Schema(ctx, datasource.SchemaRequest{}, schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("schema: %v", schemaResp.Diagnostics)
	}
	sch := schemaResp.Schema

	objType, ok := sch.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatal("schema type is not an object")
	}

	vals := make(map[string]tftypes.Value, len(objType.AttributeTypes))
	for name, typ := range objType.AttributeTypes {
		vals[name] = tftypes.NewValue(typ, nil)
	}
	if id != nil {
		vals["id"] = tftypes.NewValue(tftypes.String, *id)
	}
	if email != nil {
		vals["email"] = tftypes.NewValue(tftypes.String, *email)
	}

	return tfsdk.Config{Schema: sch, Raw: tftypes.NewValue(objType, vals)}
}

func validateConfig(t *testing.T, d *userDataSource, cfg tfsdk.Config) *datasource.ValidateConfigResponse {
	t.Helper()
	ctx := context.Background()
	resp := &datasource.ValidateConfigResponse{}
	for _, v := range d.ConfigValidators(ctx) {
		v.ValidateDataSource(ctx, datasource.ValidateConfigRequest{Config: cfg}, resp)
	}
	return resp
}

func ptr(s string) *string { return &s }

// --- AC-1c / AC-1d: exactly-one-of validation, before any API call ---

func TestUserDataSource_RejectsBothIDAndEmail(t *testing.T) {
	d := &userDataSource{}
	resp := validateConfig(t, d, userConfig(t, d, ptr("W012A3CDE"), ptr("a@b.com")))

	if !resp.Diagnostics.HasError() {
		t.Fatal("setting both id and email must be a config error")
	}
}

func TestUserDataSource_RejectsNeitherIDNorEmail(t *testing.T) {
	d := &userDataSource{}
	resp := validateConfig(t, d, userSchema(t, d))

	if !resp.Diagnostics.HasError() {
		t.Fatal("setting neither id nor email must be a config error")
	}
}

func TestUserDataSource_AcceptsIDAlone(t *testing.T) {
	d := &userDataSource{}
	resp := validateConfig(t, d, userConfig(t, d, ptr("W012A3CDE"), nil))

	if resp.Diagnostics.HasError() {
		t.Fatalf("id alone must be valid, got: %v", resp.Diagnostics)
	}
}

func TestUserDataSource_AcceptsEmailAlone(t *testing.T) {
	d := &userDataSource{}
	resp := validateConfig(t, d, userConfig(t, d, nil, ptr("a@b.com")))

	if resp.Diagnostics.HasError() {
		t.Fatalf("email alone must be valid, got: %v", resp.Diagnostics)
	}
}

func TestUserDataSource_MetadataTypeName(t *testing.T) {
	d := &userDataSource{}
	resp := &datasource.MetadataResponse{}
	d.Metadata(context.Background(), datasource.MetadataRequest{ProviderTypeName: "slack"}, resp)

	if resp.TypeName != "slack_user" {
		t.Errorf("TypeName = %q, want slack_user", resp.TypeName)
	}
}

// --- Read ---

func readUser(t *testing.T, d *userDataSource, cfg tfsdk.Config) *datasource.ReadResponse {
	t.Helper()
	ctx := context.Background()
	objType := cfg.Schema.Type().TerraformType(ctx).(tftypes.Object)
	resp := &datasource.ReadResponse{
		State: tfsdk.State{Schema: cfg.Schema, Raw: tftypes.NewValue(objType, nil)},
	}
	d.Read(ctx, datasource.ReadRequest{Config: cfg}, resp)
	return resp
}

// AC-1a
func TestUserDataSourceRead_ByID(t *testing.T) {
	d := &userDataSource{client: newStubClient(t, map[string]stub{
		"/api/users.info": fixture("users_info_full.json"),
	})}
	resp := readUser(t, d, userConfig(t, d, ptr("W012A3CDE"), nil))

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	var m userDataSourceModel
	if diags := resp.State.Get(context.Background(), &m); diags.HasError() {
		t.Fatalf("reading state: %v", diags)
	}
	if m.ID.ValueString() != "W012A3CDE" {
		t.Errorf("id = %q, want W012A3CDE", m.ID.ValueString())
	}
	if m.Name.ValueString() != "spengler" {
		t.Errorf("name = %q, want spengler", m.Name.ValueString())
	}
	if m.RealName.ValueString() != "Egon Spengler" {
		t.Errorf("real_name = %q, want Egon Spengler", m.RealName.ValueString())
	}
	if !m.IsAdmin.ValueBool() {
		t.Error("is_admin = false, want true")
	}
	if m.TZOffset.ValueInt64() != -25200 {
		t.Errorf("tz_offset = %d, want -25200", m.TZOffset.ValueInt64())
	}
	// email is mirrored to the top level from the profile
	if m.Email.ValueString() != "spengler@ghostbusters.example.com" {
		t.Errorf("email = %q, want the address", m.Email.ValueString())
	}
}

// AC-1b
func TestUserDataSourceRead_ByEmail(t *testing.T) {
	d := &userDataSource{client: newStubClient(t, map[string]stub{
		"/api/users.lookupByEmail": fixture("users_info_full.json"),
	})}
	resp := readUser(t, d, userConfig(t, d, nil, ptr("spengler@ghostbusters.example.com")))

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	var m userDataSourceModel
	if diags := resp.State.Get(context.Background(), &m); diags.HasError() {
		t.Fatalf("reading state: %v", diags)
	}
	// id is populated even though email was the input
	if m.ID.ValueString() != "W012A3CDE" {
		t.Errorf("id = %q, want it populated from the response", m.ID.ValueString())
	}
}

// AC-1e -- the criterion the A-1 defect made untestable.
func TestUserDataSourceRead_NotFoundFailsTheApply(t *testing.T) {
	d := &userDataSource{client: newStubClient(t, map[string]stub{
		"/api/users.info": fixture("err_users_not_found.json"),
	})}
	resp := readUser(t, d, userConfig(t, d, ptr("U000000000"), nil))

	if !resp.Diagnostics.HasError() {
		t.Fatal("an unknown user must fail the apply, not return an empty user")
	}
	detail := resp.Diagnostics.Errors()[0].Detail() + resp.Diagnostics.Errors()[0].Summary()
	if !strings.Contains(strings.ToLower(detail), "not found") {
		t.Errorf("diagnostic should say the user was not found, got: %s", detail)
	}
}

// AC-4: the diagnostic must name the scope to add.
func TestUserDataSourceRead_MissingScopeNamesTheScope(t *testing.T) {
	d := &userDataSource{client: newStubClient(t, map[string]stub{
		"/api/users.lookupByEmail": fixture("err_missing_scope.json"),
	})}
	resp := readUser(t, d, userConfig(t, d, nil, ptr("a@b.com")))

	if !resp.Diagnostics.HasError() {
		t.Fatal("missing_scope must produce a diagnostic")
	}
	detail := resp.Diagnostics.Errors()[0].Detail()
	if !strings.Contains(detail, "users:read.email") {
		t.Errorf("diagnostic must name users:read.email, got: %s", detail)
	}
}

// AC-2c / FR-8: a successful read without the email scope yields a null email, not an
// error and not an empty string.
func TestUserDataSourceRead_AbsentEmailIsNull(t *testing.T) {
	d := &userDataSource{client: newStubClient(t, map[string]stub{
		"/api/users.info": fixture("users_info_no_email_scope.json"),
	})}
	resp := readUser(t, d, userConfig(t, d, ptr("W012A3CDE"), nil))

	if resp.Diagnostics.HasError() {
		t.Fatalf("a missing email field must not be an error: %v", resp.Diagnostics)
	}

	var m userDataSourceModel
	if diags := resp.State.Get(context.Background(), &m); diags.HasError() {
		t.Fatalf("reading state: %v", diags)
	}
	if !m.Email.IsNull() {
		t.Errorf("email = %q, want null when Slack omits it", m.Email.ValueString())
	}
}

func TestUserDataSourceRead_InvalidAuthIsDiagnosed(t *testing.T) {
	d := &userDataSource{client: newStubClient(t, map[string]stub{
		"/api/users.info": fixture("err_invalid_auth.json"),
	})}
	resp := readUser(t, d, userConfig(t, d, ptr("W012A3CDE"), nil))

	if !resp.Diagnostics.HasError() {
		t.Fatal("invalid_auth must produce a diagnostic")
	}
}
