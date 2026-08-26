package slackclient

import (
	"encoding/json"
	"testing"
)

func decodeUserGroups(t *testing.T, name string) []UserGroup {
	t.Helper()
	var res userGroupListResponse
	if err := json.Unmarshal(loadFixture(t, name), &res); err != nil {
		t.Fatalf("unmarshalling %s: %v", name, err)
	}
	return res.UserGroups
}

func TestUserGroup_UnmarshalsListResponse(t *testing.T) {
	groups := decodeUserGroups(t, "usergroups_list_ok.json")

	if len(groups) != 2 {
		t.Fatalf("len = %d, want 2", len(groups))
	}
	g := groups[0]
	if g.ID != "S0615G0KT" {
		t.Errorf("ID = %q, want S0615G0KT", g.ID)
	}
	if g.Name != "Marketing Team" {
		t.Errorf("Name = %q, want Marketing Team", g.Name)
	}
	if g.Handle != "marketing-team" {
		t.Errorf("Handle = %q, want marketing-team", g.Handle)
	}
	if g.Description == nil || *g.Description == "" {
		t.Errorf("Description = %v, want the description text", g.Description)
	}
	if g.TeamID != "T060RNRCH" {
		t.Errorf("TeamID = %q, want T060RNRCH", g.TeamID)
	}
	if len(g.Users) != 2 || g.Users[0] != "U060RNRCZ" {
		t.Errorf("Users = %v, want the two member IDs", g.Users)
	}
	if g.Prefs == nil || len(g.Prefs.Channels) != 1 || g.Prefs.Channels[0] != "C0611AAAA" {
		t.Errorf("Prefs.Channels = %v, want [C0611AAAA]", g.Prefs)
	}
}

// Slack has no delete: a disabled group is signalled by a non-zero date_delete.
// This is the single most important predicate in the whole track -- adopt-on-create
// depends on it.
func TestUserGroup_IsDisabledFromDateDelete(t *testing.T) {
	groups := decodeUserGroups(t, "usergroups_list_ok.json")

	if groups[0].IsDisabled() {
		t.Error("active group (date_delete=0) reported as disabled")
	}
	if !groups[1].IsDisabled() {
		t.Error("disabled group (date_delete>0) reported as active")
	}
}

// FR-9a: a response with no prefs key must not panic and must be distinguishable from
// one carrying an empty channel list, so Read can decide whether to carry forward.
func TestUserGroup_MissingPrefsIsNil(t *testing.T) {
	groups := decodeUserGroups(t, "usergroups_list_no_prefs.json")

	if len(groups) != 1 {
		t.Fatalf("len = %d, want 1", len(groups))
	}
	if groups[0].Prefs != nil {
		t.Errorf("Prefs = %v, want nil when the key is absent", groups[0].Prefs)
	}
}

// Slack is inconsistent about user_count across endpoints -- sometimes a JSON number,
// sometimes a quoted string. Both must decode.
func TestUserGroup_UserCountAcceptsNumberOrString(t *testing.T) {
	var asNumber UserGroup
	if err := json.Unmarshal([]byte(`{"id":"S1","user_count":7}`), &asNumber); err != nil {
		t.Fatalf("numeric user_count: %v", err)
	}
	if asNumber.UserCount == nil || *asNumber.UserCount != 7 {
		t.Errorf("numeric UserCount = %v, want 7", asNumber.UserCount)
	}

	var asString UserGroup
	if err := json.Unmarshal([]byte(`{"id":"S1","user_count":"7"}`), &asString); err != nil {
		t.Fatalf("string user_count: %v", err)
	}
	if asString.UserCount == nil || *asString.UserCount != 7 {
		t.Errorf("string UserCount = %v, want 7", asString.UserCount)
	}

	var absent UserGroup
	if err := json.Unmarshal([]byte(`{"id":"S1"}`), &absent); err != nil {
		t.Fatalf("absent user_count: %v", err)
	}
	if absent.UserCount != nil {
		t.Errorf("absent UserCount = %v, want nil", absent.UserCount)
	}
}

// --- fields discovered from a live usergroups.list response (Phase 1, Task 1.6) ---
//
// The hand-built fixture was missing ten fields Slack actually returns. Two of them
// determine whether membership is ours to manage at all.

func TestUserGroup_DecodesRecordedResponseShape(t *testing.T) {
	groups := decodeUserGroups(t, "usergroups_list_recorded.json")

	if len(groups) != 3 {
		t.Fatalf("len = %d, want 3", len(groups))
	}
	g := groups[0]

	if g.ChannelCount == nil || g.ChannelCount.Int64() != 1 {
		t.Errorf("ChannelCount = %v, want 1", g.ChannelCount)
	}
	if g.IsVisible == nil {
		t.Error("IsVisible = nil, want it decoded")
	}
	if g.IsSubteam == nil {
		t.Error("IsSubteam = nil, want it decoded")
	}
	if g.IsOrgLevel == nil {
		t.Error("IsOrgLevel = nil, want it decoded")
	}
	if g.EnterpriseSubteamID == nil {
		t.Error("EnterpriseSubteamID = nil, want it decoded (empty string in the fixture)")
	}
}

// AC-2 depends on membership being ours to manage. A group synced from an identity
// provider, or with membership locked, is not -- applying `users` there would either
// fail or fight the IdP.
func TestUserGroup_MembershipManageability(t *testing.T) {
	groups := decodeUserGroups(t, "usergroups_list_recorded.json")
	active, idp := groups[0], groups[2]

	if !active.MembershipIsManageable() {
		t.Error("ordinary group reported as unmanageable")
	}
	if idp.MembershipIsManageable() {
		t.Error("IdP-synced, membership-locked group reported as manageable")
	}

	if idp.IsIDPGroup == nil || !*idp.IsIDPGroup {
		t.Errorf("IsIDPGroup = %v, want true", idp.IsIDPGroup)
	}
	if idp.IsMembershipLocked == nil || !*idp.IsMembershipLocked {
		t.Errorf("IsMembershipLocked = %v, want true", idp.IsMembershipLocked)
	}
}

// The disabled entry in the recorded-shape fixture must still read as disabled.
func TestUserGroup_RecordedDisabledGroup(t *testing.T) {
	groups := decodeUserGroups(t, "usergroups_list_recorded.json")

	if groups[0].IsDisabled() {
		t.Error("active group reported as disabled")
	}
	if !groups[1].IsDisabled() {
		t.Error("disabled group reported as active")
	}
}
