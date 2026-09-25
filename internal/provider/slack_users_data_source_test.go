package provider

// Tasks 3.1-3.2: the slack_users schema and its selector validation.

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func usersSchema(t *testing.T) datasource.SchemaResponse {
	t.Helper()
	d := &usersDataSource{}
	resp := datasource.SchemaResponse{}
	d.Schema(context.Background(), datasource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema: %v", resp.Diagnostics)
	}
	return resp
}

func TestUsersDataSource_SchemaHasExpectedAttributes(t *testing.T) {
	attrs := usersSchema(t).Schema.Attributes

	for _, name := range []string{
		"emails", "usernames", "channel",
		"is_bot", "deleted", "is_restricted", "is_ultra_restricted", "is_admin", "is_app_user",
		"error_on_no_match",
		"users", "user_ids", "user_count",
	} {
		if _, ok := attrs[name]; !ok {
			t.Errorf("schema is missing attribute %q", name)
		}
	}

	// A-11: nothing non-deterministic.
	if _, ok := attrs["last_updated"]; ok {
		t.Error(`schema exposes "last_updated", which is non-deterministic (guardrail A-11)`)
	}
	// FR-1: "count" is reserved by the framework for data sources.
	if _, ok := attrs["count"]; ok {
		t.Error(`schema exposes "count", which the framework reserves; use "user_count"`)
	}
}

// AC-16. A reserved attribute name is not a naming preference -- it fails schema
// validation at provider startup, which is a broken provider, not a bad plan. This is
// the only check that catches it, so it runs against every registered type rather than
// just the new one.
func TestProvider_EverySchemaValidates(t *testing.T) {
	ctx := context.Background()
	p := &slackProvider{}

	for _, newDS := range p.DataSources(ctx) {
		ds := newDS()

		mdResp := datasource.MetadataResponse{}
		ds.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "slack"}, &mdResp)

		schemaResp := datasource.SchemaResponse{}
		ds.Schema(ctx, datasource.SchemaRequest{}, &schemaResp)
		if schemaResp.Diagnostics.HasError() {
			t.Errorf("%s: building the schema failed: %v", mdResp.TypeName, schemaResp.Diagnostics)
			continue
		}

		if diags := schemaResp.Schema.ValidateImplementation(ctx); diags.HasError() {
			t.Errorf("%s: schema is not a valid implementation: %v", mdResp.TypeName, diags)
		}
	}
}

// usersConfig builds a config with every attribute null except the ones supplied.
func usersConfig(t *testing.T, sel map[string]tftypes.Value) tfsdk.Config {
	t.Helper()
	ctx := context.Background()
	sch := usersSchema(t).Schema

	objType, ok := sch.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatal("schema type is not an object")
	}

	vals := make(map[string]tftypes.Value, len(objType.AttributeTypes))
	for name, typ := range objType.AttributeTypes {
		vals[name] = tftypes.NewValue(typ, nil)
	}
	for name, v := range sel {
		if _, ok := objType.AttributeTypes[name]; !ok {
			t.Fatalf("test sets unknown attribute %q", name)
		}
		vals[name] = v
	}

	return tfsdk.Config{Schema: sch, Raw: tftypes.NewValue(objType, vals)}
}

func tfStringSet(values ...string) tftypes.Value {
	elems := make([]tftypes.Value, 0, len(values))
	for _, v := range values {
		elems = append(elems, tftypes.NewValue(tftypes.String, v))
	}
	return tftypes.NewValue(tftypes.Set{ElementType: tftypes.String}, elems)
}

func validateUsersConfig(t *testing.T, cfg tfsdk.Config) *datasource.ValidateConfigResponse {
	t.Helper()
	ctx := context.Background()
	d := &usersDataSource{}
	resp := &datasource.ValidateConfigResponse{}
	for _, v := range d.ConfigValidators(ctx) {
		v.ValidateDataSource(ctx, datasource.ValidateConfigRequest{Config: cfg}, resp)
	}
	return resp
}

// AC-17 / FR-2.

func TestUsersDataSource_AcceptsNoSelector(t *testing.T) {
	resp := validateUsersConfig(t, usersConfig(t, nil))

	if resp.Diagnostics.HasError() {
		t.Fatalf("no selector means the whole workspace and must be valid, got: %v", resp.Diagnostics)
	}
}

func TestUsersDataSource_AcceptsOneSelector(t *testing.T) {
	for name, val := range map[string]tftypes.Value{
		"emails":    tfStringSet("a@b.com"),
		"usernames": tfStringSet("spengler"),
		"channel":   tftypes.NewValue(tftypes.String, "C012AB3CD"),
	} {
		resp := validateUsersConfig(t, usersConfig(t, map[string]tftypes.Value{name: val}))
		if resp.Diagnostics.HasError() {
			t.Errorf("%s alone must be valid, got: %v", name, resp.Diagnostics)
		}
	}
}

func TestUsersDataSource_RejectsTwoSelectors(t *testing.T) {
	pairs := []map[string]tftypes.Value{
		{"emails": tfStringSet("a@b.com"), "usernames": tfStringSet("spengler")},
		{"emails": tfStringSet("a@b.com"), "channel": tftypes.NewValue(tftypes.String, "C012AB3CD")},
		{"usernames": tfStringSet("spengler"), "channel": tftypes.NewValue(tftypes.String, "C012AB3CD")},
	}
	for _, sel := range pairs {
		resp := validateUsersConfig(t, usersConfig(t, sel))
		if !resp.Diagnostics.HasError() {
			names := make([]string, 0, len(sel))
			for k := range sel {
				names = append(names, k)
			}
			t.Errorf("setting %v together must be a config error", names)
		}
	}
}

func TestUsersDataSource_MetadataTypeName(t *testing.T) {
	d := &usersDataSource{}
	resp := datasource.MetadataResponse{}
	d.Metadata(context.Background(), datasource.MetadataRequest{ProviderTypeName: "slack"}, &resp)

	if resp.TypeName != "slack_users" {
		t.Errorf("TypeName = %q, want slack_users", resp.TypeName)
	}
}
