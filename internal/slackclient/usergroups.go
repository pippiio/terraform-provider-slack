package slackclient

// Story: Slack user groups (subteams)
//
// Input:  responses from the usergroups.* family of Web API methods.
// Process:
//   1. Decode the subteam object, keeping optional fields as pointers so an absent
//      value stays distinguishable from an empty one.
//   2. Expose IsDisabled(), derived from date_delete, because Slack has no delete and
//      "disabled" is the closest thing to a removed group.
// Output: *UserGroup values the provider layer maps onto the Terraform schema.
//
// Dependencies: encoding/json, strconv.
// Side effects: none.
//
// Slack constraints encoded here:
//   - There is no usergroups.delete. Groups are disabled, and a disabled group keeps
//     its name and handle reserved. IsDisabled() is how the provider detects this.
//   - user_count is returned as a JSON number by some endpoints and a quoted string by
//     others, so it needs a tolerant unmarshaller.

import (
	"bytes"
	"encoding/json"
	"strconv"
)

// UserGroup is a Slack user group (the API calls these "subteams").
type UserGroup struct {
	ID          string  `json:"id"`
	TeamID      string  `json:"team_id"`
	IsUserGroup *bool   `json:"is_usergroup"`
	Name        string  `json:"name"`
	Handle      string  `json:"handle"`
	Description *string `json:"description"`
	IsExternal  *bool   `json:"is_external"`

	DateCreate *int64 `json:"date_create"`
	DateUpdate *int64 `json:"date_update"`
	// DateDelete is 0 for an active group and a timestamp for a disabled one.
	DateDelete *int64 `json:"date_delete"`

	CreatedBy *string `json:"created_by"`
	UpdatedBy *string `json:"updated_by"`
	DeletedBy *string `json:"deleted_by"`

	// Prefs is nil when Slack omits the key entirely, which is distinct from Slack
	// returning an empty channel list. The provider relies on that distinction to decide
	// whether to carry a stored value forward rather than blanking it (FR-9a).
	Prefs *UserGroupPrefs `json:"prefs"`

	// Users is populated only when the request asked for include_users.
	Users []string `json:"users"`

	UserCount    *flexInt64 `json:"user_count"`
	ChannelCount *flexInt64 `json:"channel_count"`

	// Flags below were discovered by capturing a live usergroups.list response during
	// Phase 1 reconnaissance; Slack's documented example omits them.
	//
	// IsIDPGroup and IsMembershipLocked are the consequential pair: a group synced from
	// an identity provider, or one whose membership is locked, is not ours to manage.
	// Applying an authoritative `users` set to either would fight the IdP or simply fail.
	IsIDPGroup          *bool   `json:"is_idp_group"`
	IsMembershipLocked  *bool   `json:"is_membership_locked"`
	IsEditingRestricted *bool   `json:"is_editing_restricted"`
	AutoProvision       *bool   `json:"auto_provision"`
	AutoType            *string `json:"auto_type"`
	IsOrgLevel          *bool   `json:"is_org_level"`
	IsSection           *bool   `json:"is_section"`
	IsSubteam           *bool   `json:"is_subteam"`
	IsVisible           *bool   `json:"is_visible"`
	EnterpriseSubteamID *string `json:"enterprise_subteam_id"`
}

// MembershipIsManageable reports whether this provider may set the group's members.
//
// It is false for groups synced from an identity provider and for groups whose
// membership Slack has locked. Writing members to either is at best futile and at worst
// a fight with the system of record, so the resource refuses rather than trying.
func (g *UserGroup) MembershipIsManageable() bool {
	if g.IsIDPGroup != nil && *g.IsIDPGroup {
		return false
	}
	if g.IsMembershipLocked != nil && *g.IsMembershipLocked {
		return false
	}
	return true
}

// UserGroupPrefs holds the group's default channels and private channels.
type UserGroupPrefs struct {
	Channels []string `json:"channels"`
	Groups   []string `json:"groups"`
}

// IsDisabled reports whether the group has been disabled.
//
// Slack offers no delete for user groups, only usergroups.disable, and a disabled group
// keeps its name and handle reserved. A non-zero date_delete is the signal.
func (g *UserGroup) IsDisabled() bool {
	return g.DateDelete != nil && *g.DateDelete > 0
}

// flexInt64 decodes a JSON value that Slack sends as either a number or a quoted string.
type flexInt64 int64

func (f *flexInt64) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil
	}
	// Strip surrounding quotes for the string form.
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return err
		}
		if s == "" {
			return nil
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return err
		}
		*f = flexInt64(n)
		return nil
	}
	var n int64
	if err := json.Unmarshal(trimmed, &n); err != nil {
		return err
	}
	*f = flexInt64(n)
	return nil
}

// Int64 returns the value as a plain int64.
func (f *flexInt64) Int64() int64 {
	if f == nil {
		return 0
	}
	return int64(*f)
}

// --- response envelopes ---

type userGroupResponse struct {
	UserGroup UserGroup `json:"usergroup"`
}

type userGroupListResponse struct {
	UserGroups []UserGroup `json:"usergroups"`
}

type userGroupUsersResponse struct {
	Users []string `json:"users"`
}
