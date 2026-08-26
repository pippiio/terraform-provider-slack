package provider

// AC-6: import matters more here than for most resources. Slack has no delete, so a
// group that already exists -- whether created by hand or left disabled by a previous
// destroy -- can only be brought under management this way.

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestUserGroupResource_ImplementsImportState(t *testing.T) {
	var r any = &userGroupResource{}
	if _, ok := r.(resource.ResourceWithImportState); !ok {
		t.Fatal("userGroupResource must implement resource.ResourceWithImportState")
	}
}

func TestUserGroupResource_ImportSetsIDFromPassthrough(t *testing.T) {
	ctx := context.Background()
	r := &userGroupResource{}

	sr := resource.SchemaResponse{}
	r.Schema(ctx, resource.SchemaRequest{}, &sr)
	objType := sr.Schema.Type().TerraformType(ctx).(tftypes.Object)

	resp := &resource.ImportStateResponse{
		State: tfsdk.State{Schema: sr.Schema, Raw: tftypes.NewValue(objType, nil)},
	}
	r.ImportState(ctx, resource.ImportStateRequest{ID: "S0615G0KT"}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("import should not error: %v", resp.Diagnostics)
	}

	var m userGroupResourceModel
	if diags := resp.State.Get(ctx, &m); diags.HasError() {
		t.Fatalf("reading imported state: %v", diags)
	}
	if m.ID.ValueString() != "S0615G0KT" {
		t.Errorf("id = %q, want S0615G0KT", m.ID.ValueString())
	}
}

// The import ID alone must be enough for Read to populate everything -- that is what
// makes the import usable rather than leaving a half-populated resource.
func TestUserGroupResource_ReadPopulatesFromIDAlone(t *testing.T) {
	ctx := context.Background()
	r := newUserGroupResource(t, map[string]stub{
		"/api/usergroups.list": fixture("usergroups_list_recorded.json"),
	})

	sr := resource.SchemaResponse{}
	r.Schema(ctx, resource.SchemaRequest{}, &sr)
	objType := sr.Schema.Type().TerraformType(ctx).(tftypes.Object)

	// State as it looks straight after import: id set, everything else null.
	vals := make(map[string]tftypes.Value, len(objType.AttributeTypes))
	for n, typ := range objType.AttributeTypes {
		vals[n] = tftypes.NewValue(typ, nil)
	}
	vals["id"] = tftypes.NewValue(tftypes.String, "S0615G0KT")
	state := tfsdk.State{Schema: sr.Schema, Raw: tftypes.NewValue(objType, vals)}

	resp := &resource.ReadResponse{State: state}
	r.Read(ctx, resource.ReadRequest{State: state}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("read after import: %v", resp.Diagnostics)
	}

	var m userGroupResourceModel
	if diags := resp.State.Get(ctx, &m); diags.HasError() {
		t.Fatalf("reading state: %v", diags)
	}
	if m.Name.ValueString() != "Example Alpha Team" {
		t.Errorf("name = %q, want it populated from the ID alone", m.Name.ValueString())
	}
	if m.Handle.ValueString() != "example-alpha" {
		t.Errorf("handle = %q", m.Handle.ValueString())
	}
	if m.TeamID.ValueString() == "" {
		t.Error("team_id should be populated after import")
	}
	// users stays null: the imported config has not opted into managing membership.
	if !m.Users.IsNull() {
		t.Errorf("users = %v, want null after import -- importing must not silently start managing membership", m.Users)
	}
}

// Importing a disabled group is the recovery path after a destroy. Read drops it from
// state, so the operator learns immediately rather than getting a broken resource.
func TestUserGroupResource_ReadRemovesDisabledGroup(t *testing.T) {
	ctx := context.Background()
	r := newUserGroupResource(t, map[string]stub{
		"/api/usergroups.list": fixture("usergroups_list_recorded.json"),
	})

	sr := resource.SchemaResponse{}
	r.Schema(ctx, resource.SchemaRequest{}, &sr)
	objType := sr.Schema.Type().TerraformType(ctx).(tftypes.Object)
	vals := make(map[string]tftypes.Value, len(objType.AttributeTypes))
	for n, typ := range objType.AttributeTypes {
		vals[n] = tftypes.NewValue(typ, nil)
	}
	vals["id"] = tftypes.NewValue(tftypes.String, "S06MMAAAA") // the disabled fixture group
	state := tfsdk.State{Schema: sr.Schema, Raw: tftypes.NewValue(objType, vals)}

	resp := &resource.ReadResponse{State: state}
	r.Read(ctx, resource.ReadRequest{State: state}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	if !resp.State.Raw.IsNull() {
		t.Error("a disabled group should be removed from state so Terraform recreates or adopts it")
	}
}
