package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// AC-8: slack_user_ids must behave exactly as before. This track adds slack_user
// alongside it and changes doRequest underneath it, so its behaviour is pinned here.

func userIdsConfig(t *testing.T, d *userIdDataSource, usernames []string) tfsdk.Config {
	t.Helper()
	ctx := context.Background()

	schemaResp := &datasource.SchemaResponse{}
	d.Schema(ctx, datasource.SchemaRequest{}, schemaResp)
	sch := schemaResp.Schema
	objType := sch.Type().TerraformType(ctx).(tftypes.Object)

	elems := make([]tftypes.Value, 0, len(usernames))
	for _, u := range usernames {
		elems = append(elems, tftypes.NewValue(tftypes.String, u))
	}

	return tfsdk.Config{Schema: sch, Raw: tftypes.NewValue(objType, map[string]tftypes.Value{
		"usernames":    tftypes.NewValue(tftypes.Set{ElementType: tftypes.String}, elems),
		"slack_ids":    tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, nil),
		"last_updated": tftypes.NewValue(tftypes.String, nil),
	})}
}

func TestUserIdsDataSource_ResolvesUsernamesUnchanged(t *testing.T) {
	ctx := context.Background()
	d := &userIdDataSource{client: newStubClient(t, map[string]stub{
		"/api/users.list": fixture("users_list_ok.json"),
	})}

	cfg := userIdsConfig(t, d, []string{"spengler", "glinda"})
	objType := cfg.Schema.Type().TerraformType(ctx).(tftypes.Object)
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: cfg.Schema, Raw: tftypes.NewValue(objType, nil)}}

	d.Read(ctx, datasource.ReadRequest{Config: cfg}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	var m userIdDataSourceModel
	if diags := resp.State.Get(ctx, &m); diags.HasError() {
		t.Fatalf("reading state: %v", diags)
	}

	ids := m.Slack_IDs.Elements()
	if len(ids) != 2 {
		t.Fatalf("len(slack_ids) = %d, want 2 (got %v)", len(ids), ids)
	}
	if got := ids["spengler"].String(); got != `"W012A3CDE"` {
		t.Errorf("slack_ids[spengler] = %s, want W012A3CDE", got)
	}
}

// The A-1 fix means users.list failures now surface. Previously an ok:false body was
// silently decoded into an empty member list and the data source returned no IDs.
func TestUserIdsDataSource_SurfacesApiErrorAfterA1Fix(t *testing.T) {
	ctx := context.Background()
	d := &userIdDataSource{client: newStubClient(t, map[string]stub{
		"/api/users.list": fixture("err_invalid_auth.json"),
	})}

	cfg := userIdsConfig(t, d, []string{"spengler"})
	objType := cfg.Schema.Type().TerraformType(ctx).(tftypes.Object)
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: cfg.Schema, Raw: tftypes.NewValue(objType, nil)}}

	d.Read(ctx, datasource.ReadRequest{Config: cfg}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("an invalid token must now fail the read instead of returning an empty map")
	}
}
