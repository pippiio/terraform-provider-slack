package slackclient

import (
	"encoding/json"
	"net/http"
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

// --- client methods ---

func TestCreateUserGroup_SendsAllFields(t *testing.T) {
	c, rec := newTestClient(t, routes{"/api/usergroups.create": fixture("usergroups_create_ok.json")})

	g, err := c.CreateUserGroup(CreateUserGroupRequest{
		Name:        "Marketing Team",
		Handle:      "marketing-team",
		Description: "Marketing gurus.",
		Channels:    []string{"C0611AAAA", "C0611BBBB"},
	})
	if err != nil {
		t.Fatalf("CreateUserGroup: %v", err)
	}
	if g.ID != "S0615G0KT" {
		t.Errorf("ID = %q, want S0615G0KT", g.ID)
	}

	req := rec.last()
	if req.Path != "/api/usergroups.create" {
		t.Errorf("path = %q", req.Path)
	}
	if req.Method != http.MethodPost {
		t.Errorf("method = %q, want POST (create is a mutation)", req.Method)
	}
	if got := req.Query.Get("name"); got != "Marketing Team" {
		t.Errorf("name = %q", got)
	}
	if got := req.Query.Get("handle"); got != "marketing-team" {
		t.Errorf("handle = %q", got)
	}
	if got := req.Query.Get("description"); got != "Marketing gurus." {
		t.Errorf("description = %q", got)
	}
	if got := req.Query.Get("channels"); got != "C0611AAAA,C0611BBBB" {
		t.Errorf("channels = %q, want a comma-separated list", got)
	}
}

func TestCreateUserGroup_OmitsEmptyOptionalFields(t *testing.T) {
	c, rec := newTestClient(t, routes{"/api/usergroups.create": fixture("usergroups_create_ok.json")})

	if _, err := c.CreateUserGroup(CreateUserGroupRequest{Name: "N", Handle: "h"}); err != nil {
		t.Fatalf("CreateUserGroup: %v", err)
	}
	q := rec.last().Query
	if _, present := q["description"]; present {
		t.Error("empty description should not be sent")
	}
	if _, present := q["channels"]; present {
		t.Error("empty channels should not be sent")
	}
}

func TestListUserGroups_SendsIncludeFlags(t *testing.T) {
	c, rec := newTestClient(t, routes{"/api/usergroups.list": fixture("usergroups_list_recorded.json")})

	groups, err := c.ListUserGroups(true, true)
	if err != nil {
		t.Fatalf("ListUserGroups: %v", err)
	}
	if len(groups) != 3 {
		t.Fatalf("len = %d, want 3", len(groups))
	}
	q := rec.last().Query
	if q.Get("include_users") != "true" {
		t.Errorf("include_users = %q, want true", q.Get("include_users"))
	}
	if q.Get("include_disabled") != "true" {
		t.Errorf("include_disabled = %q, want true", q.Get("include_disabled"))
	}
}

// FR-3's adopt branch needs to find a group by handle, including disabled ones.
func TestFindUserGroupByHandle(t *testing.T) {
	c, _ := newTestClient(t, routes{"/api/usergroups.list": fixture("usergroups_list_recorded.json")})

	found, err := c.FindUserGroupByHandle("example-retired")
	if err != nil {
		t.Fatalf("FindUserGroupByHandle: %v", err)
	}
	if found == nil {
		t.Fatal("disabled group not found -- adopt-on-create depends on this")
	}
	if !found.IsDisabled() {
		t.Error("found group should report as disabled")
	}

	missing, err := c.FindUserGroupByHandle("no-such-handle")
	if err != nil {
		t.Fatalf("FindUserGroupByHandle(missing): %v", err)
	}
	if missing != nil {
		t.Errorf("expected nil for an unknown handle, got %+v", missing)
	}
}

func TestUpdateUserGroup_SendsOnlySetFields(t *testing.T) {
	c, rec := newTestClient(t, routes{"/api/usergroups.update": fixture("usergroups_create_ok.json")})

	name := "New Name"
	if _, err := c.UpdateUserGroup(UpdateUserGroupRequest{ID: "S0615G0KT", Name: &name}); err != nil {
		t.Fatalf("UpdateUserGroup: %v", err)
	}
	q := rec.last().Query
	if q.Get("usergroup") != "S0615G0KT" {
		t.Errorf("usergroup = %q", q.Get("usergroup"))
	}
	if q.Get("name") != "New Name" {
		t.Errorf("name = %q", q.Get("name"))
	}
	if _, present := q["handle"]; present {
		t.Error("unset handle should not be sent -- a nil pointer means 'leave unchanged'")
	}
}

func TestDisableAndEnableUserGroup(t *testing.T) {
	c, rec := newTestClient(t, routes{
		"/api/usergroups.disable": fixture("usergroups_disable_ok.json"),
		"/api/usergroups.enable":  fixture("usergroups_create_ok.json"),
	})

	disabled, err := c.DisableUserGroup("S0615G0KT")
	if err != nil {
		t.Fatalf("DisableUserGroup: %v", err)
	}
	if !disabled.IsDisabled() {
		t.Error("disable response should report the group as disabled")
	}
	if rec.last().Method != http.MethodPost {
		t.Errorf("disable method = %q, want POST", rec.last().Method)
	}

	enabled, err := c.EnableUserGroup("S0615G0KT")
	if err != nil {
		t.Fatalf("EnableUserGroup: %v", err)
	}
	if enabled.IsDisabled() {
		t.Error("enable response should report the group as active")
	}
}

func TestUpdateUserGroupUsers_JoinsIDs(t *testing.T) {
	c, rec := newTestClient(t, routes{"/api/usergroups.users.update": fixture("usergroups_users_update_ok.json")})

	g, err := c.UpdateUserGroupUsers("S0615G0KT", []string{"U060RNRCZ", "U060ULRC0"})
	if err != nil {
		t.Fatalf("UpdateUserGroupUsers: %v", err)
	}
	if len(g.Users) != 2 {
		t.Errorf("Users = %v, want 2", g.Users)
	}
	q := rec.last().Query
	if q.Get("usergroup") != "S0615G0KT" {
		t.Errorf("usergroup = %q", q.Get("usergroup"))
	}
	if q.Get("users") != "U060RNRCZ,U060ULRC0" {
		t.Errorf("users = %q, want a comma-separated list", q.Get("users"))
	}
}

func TestListUserGroupUsers(t *testing.T) {
	c, rec := newTestClient(t, routes{"/api/usergroups.users.list": fixture("usergroups_users_list_ok.json")})

	users, err := c.ListUserGroupUsers("S0615G0KT")
	if err != nil {
		t.Fatalf("ListUserGroupUsers: %v", err)
	}
	if len(users) != 2 || users[0] != "U060RNRCZ" {
		t.Errorf("users = %v", users)
	}
	if rec.last().Query.Get("usergroup") != "S0615G0KT" {
		t.Error("usergroup param not sent")
	}
}

// Every method must surface ok:false, not swallow it -- the NFR-3 lesson from PR #14,
// where testing doRequest alone did not prove its callers safe.
func TestUserGroupMethods_SurfaceOkFalse(t *testing.T) {
	t.Run("create/paid_only", func(t *testing.T) {
		c, _ := newTestClient(t, routes{"/api/usergroups.create": fixture("err_paid_only.json")})
		_, err := c.CreateUserGroup(CreateUserGroupRequest{Name: "n", Handle: "h"})
		if ErrorCode(err) != "paid_only" {
			t.Errorf("ErrorCode = %q, want paid_only", ErrorCode(err))
		}
	})
	t.Run("create/permission_denied", func(t *testing.T) {
		c, _ := newTestClient(t, routes{"/api/usergroups.create": fixture("err_permission_denied.json")})
		_, err := c.CreateUserGroup(CreateUserGroupRequest{Name: "n", Handle: "h"})
		if ErrorCode(err) != "permission_denied" {
			t.Errorf("ErrorCode = %q, want permission_denied", ErrorCode(err))
		}
	})
	t.Run("list/missing_scope", func(t *testing.T) {
		c, _ := newTestClient(t, routes{"/api/usergroups.list": fixture("err_missing_scope.json")})
		_, err := c.ListUserGroups(false, false)
		if ErrorCode(err) != "missing_scope" {
			t.Errorf("ErrorCode = %q, want missing_scope", ErrorCode(err))
		}
	})
	t.Run("update/no_such_subteam", func(t *testing.T) {
		c, _ := newTestClient(t, routes{"/api/usergroups.update": fixture("err_no_such_subteam.json")})
		n := "x"
		_, err := c.UpdateUserGroup(UpdateUserGroupRequest{ID: "S1", Name: &n})
		if ErrorCode(err) != "no_such_subteam" {
			t.Errorf("ErrorCode = %q, want no_such_subteam", ErrorCode(err))
		}
	})
	t.Run("users.update/invalid_users", func(t *testing.T) {
		c, _ := newTestClient(t, routes{"/api/usergroups.users.update": fixture("err_invalid_users.json")})
		_, err := c.UpdateUserGroupUsers("S1", []string{"nope"})
		if ErrorCode(err) != "invalid_users" {
			t.Errorf("ErrorCode = %q, want invalid_users", ErrorCode(err))
		}
	})
	t.Run("disable/permission_denied", func(t *testing.T) {
		c, _ := newTestClient(t, routes{"/api/usergroups.disable": fixture("err_permission_denied.json")})
		_, err := c.DisableUserGroup("S1")
		if ErrorCode(err) != "permission_denied" {
			t.Errorf("ErrorCode = %q, want permission_denied", ErrorCode(err))
		}
	})
}
