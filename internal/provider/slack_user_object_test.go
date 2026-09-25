package provider

// FR-11: slack_user and slack_users must expose the same user object. These tests are
// the drift guard -- two hand-maintained copies of a 27-field profile would not stay
// identical for a release, and nothing else would notice until a plan diffed wrong.

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// modelTFSDKNames returns the tfsdk attribute names declared on userDataSourceModel.
func modelTFSDKNames(t *testing.T) map[string]bool {
	t.Helper()
	typ := reflect.TypeOf(userDataSourceModel{})
	names := make(map[string]bool, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		tag, ok := typ.Field(i).Tag.Lookup("tfsdk")
		if !ok {
			t.Fatalf("field %s has no tfsdk tag", typ.Field(i).Name)
		}
		names[tag] = true
	}
	return names
}

// userAttrTypes() feeds the element type of slack_users' map. If it drifts from the
// model by even one key, ObjectValueFrom fails at runtime on every read -- so pin it
// here, where the failure is a test rather than a broken plan.
func TestUserAttrTypes_MatchesModelExactly(t *testing.T) {
	want := modelTFSDKNames(t)
	got := userAttrTypes()

	for name := range want {
		if _, ok := got[name]; !ok {
			t.Errorf("userAttrTypes() is missing %q, which userDataSourceModel declares", name)
		}
	}
	for name := range got {
		if !want[name] {
			t.Errorf("userAttrTypes() has %q, which userDataSourceModel does not declare", name)
		}
	}
}

func TestUserToObject_CarriesTheWholeUser(t *testing.T) {
	ctx := context.Background()
	c := newStubClient(t, map[string]stub{
		"/api/users.info": fixture("users_info_full.json"),
	})

	u, err := c.GetUserByID("W012A3CDE")
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}

	obj, diags := userToObject(ctx, u)
	if diags.HasError() {
		t.Fatalf("userToObject: %v", diags)
	}

	attrs := obj.Attributes()
	if got := attrs["id"]; got.String() != `"W012A3CDE"` {
		t.Errorf("id = %s, want W012A3CDE", got)
	}
	if got := attrs["name"]; got.String() != `"spengler"` {
		t.Errorf("name = %s, want spengler", got)
	}
	if got := attrs["is_admin"]; got.String() != "true" {
		t.Errorf("is_admin = %s, want true", got)
	}

	// The nested profile has to survive the round trip, not just the flat fields --
	// it is the half most likely to drift.
	profile, ok := attrs["profile"]
	if !ok {
		t.Fatal("object has no profile attribute")
	}
	if profile.IsNull() {
		t.Fatal("profile is null; the nested object was not carried through")
	}
	if !strings.Contains(profile.String(), "Paranormal Investigator") {
		t.Errorf("profile does not carry the title: %s", profile.String())
	}
}

// A null-heavy user must not lose the null/empty distinction on the way into an object.
func TestUserToObject_PreservesNulls(t *testing.T) {
	ctx := context.Background()
	c := newStubClient(t, map[string]stub{
		"/api/users.info": fixture("users_info_no_email_scope.json"),
	})

	u, err := c.GetUserByID("W012A3CDE")
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}

	obj, diags := userToObject(ctx, u)
	if diags.HasError() {
		t.Fatalf("userToObject: %v", diags)
	}

	if got := obj.Attributes()["email"]; !got.IsNull() {
		t.Errorf("email = %s, want null when Slack omitted it", got)
	}
}
