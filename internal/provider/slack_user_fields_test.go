package provider

// profile.fields is exposed as a dynamically-keyed map of field ID -> {value, alt}.
// The field IDs are tenant-defined so they cannot appear in the schema, but the inner
// shape is stable, so the map is statically typed.

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// profileAttr pulls one attribute out of the nested profile object in state.
func profileAttr(t *testing.T, d *userDataSource, fixtureName, attrName string) attr.Value {
	t.Helper()
	ds := &userDataSource{client: newStubClient(t, map[string]stub{
		"/api/users.info": fixture(fixtureName),
	})}
	resp := readUser(t, ds, userConfig(t, ds, ptr("W012A3CDE"), nil))
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	var m userDataSourceModel
	if diags := resp.State.Get(context.Background(), &m); diags.HasError() {
		t.Fatalf("reading state: %v", diags)
	}
	v, ok := m.Profile.Attributes()[attrName]
	if !ok {
		t.Fatalf("profile has no attribute %q; got %v", attrName, m.Profile.Attributes())
	}
	return v
}

func TestUserDataSource_ProfileFieldsPopulated(t *testing.T) {
	v := profileAttr(t, &userDataSource{}, "users_info_with_excluded_fields.json", "fields")

	m, ok := v.(types.Map)
	if !ok {
		t.Fatalf("profile.fields is %T, want types.Map", v)
	}
	if m.IsNull() {
		t.Fatal("profile.fields is null, want a populated map")
	}

	entry, ok := m.Elements()["Xf0123456"]
	if !ok {
		t.Fatalf("missing key Xf0123456; got %v", m.Elements())
	}
	obj, ok := entry.(types.Object)
	if !ok {
		t.Fatalf("entry is %T, want types.Object", entry)
	}
	if got := obj.Attributes()["value"].(types.String).ValueString(); got != "Platform" {
		t.Errorf("value = %q, want Platform", got)
	}
	if obj.Attributes()["alt"].(types.String).IsNull() {
		t.Error("alt should be an empty string (present but blank), not null")
	}
}

// Slack's [] form means "no custom fields" -- an empty map, distinct from null.
func TestUserDataSource_ProfileFieldsEmptyArrayIsEmptyMap(t *testing.T) {
	v := profileAttr(t, &userDataSource{}, "users_info_empty_fields.json", "fields")

	m := v.(types.Map)
	if m.IsNull() {
		t.Fatal("profile.fields is null for the [] form, want an empty map")
	}
	if len(m.Elements()) != 0 {
		t.Errorf("len = %d, want 0", len(m.Elements()))
	}
}

// Absent from the response -> null, so config can tell it apart from "no fields".
func TestUserDataSource_ProfileFieldsAbsentIsNull(t *testing.T) {
	v := profileAttr(t, &userDataSource{}, "users_info_full.json", "fields")

	if !v.(types.Map).IsNull() {
		t.Errorf("profile.fields = %v, want null when the response omits the key", v)
	}
}
