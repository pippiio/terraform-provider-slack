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
	"fmt"
	"net/http"
	"strconv"
	"strings"
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

// --- client methods ---
//
// All follow the package's request template and route through doRequest, so Slack's
// ok:false responses surface as *SlackError.

// CreateUserGroupRequest describes a new user group.
type CreateUserGroupRequest struct {
	Name        string
	Handle      string
	Description string
	Channels    []string
}

// UpdateUserGroupRequest describes a change to an existing group. Nil pointers mean
// "leave unchanged" — Slack treats an omitted parameter as no-op, but an empty string
// as a request to clear the value, so the distinction matters.
type UpdateUserGroupRequest struct {
	ID          string
	Name        *string
	Handle      *string
	Description *string
	// Channels nil means leave unchanged; a non-nil empty slice clears the list.
	Channels []string
}

// newUserGroupRequest builds a POST with bearer auth and the given query params.
func (c *Client) newUserGroupRequest(endpoint string, params map[string]string) (*http.Request, error) {
	req, err := http.NewRequest("POST", fmt.Sprintf("%s/api/%s", c.Host, endpoint), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.Token))

	q := req.URL.Query()
	for k, v := range params {
		q.Add(k, v)
	}
	req.URL.RawQuery = q.Encode()
	return req, nil
}

// decodeUserGroup runs a request and decodes the {"usergroup": {...}} envelope.
func (c *Client) decodeUserGroup(req *http.Request) (*UserGroup, error) {
	body, err := c.doRequest(req)
	if err != nil {
		return nil, err
	}
	res := userGroupResponse{}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, err
	}
	return &res.UserGroup, nil
}

// CreateUserGroup creates a user group via usergroups.create.
//
// Requires usergroups:write. Slack also gates group creation on a workspace setting, so
// a correctly-scoped token can still receive permission_denied.
func (c *Client) CreateUserGroup(in CreateUserGroupRequest) (*UserGroup, error) {
	params := map[string]string{
		"name":   in.Name,
		"handle": in.Handle,
	}
	if in.Description != "" {
		params["description"] = in.Description
	}
	if len(in.Channels) > 0 {
		params["channels"] = strings.Join(in.Channels, ",")
	}

	req, err := c.newUserGroupRequest("usergroups.create", params)
	if err != nil {
		return nil, err
	}
	return c.decodeUserGroup(req)
}

// UpdateUserGroup updates a group's metadata via usergroups.update.
func (c *Client) UpdateUserGroup(in UpdateUserGroupRequest) (*UserGroup, error) {
	params := map[string]string{"usergroup": in.ID}
	if in.Name != nil {
		params["name"] = *in.Name
	}
	if in.Handle != nil {
		params["handle"] = *in.Handle
	}
	if in.Description != nil {
		params["description"] = *in.Description
	}
	if in.Channels != nil {
		params["channels"] = strings.Join(in.Channels, ",")
	}

	req, err := c.newUserGroupRequest("usergroups.update", params)
	if err != nil {
		return nil, err
	}
	return c.decodeUserGroup(req)
}

// ListUserGroups lists the workspace's user groups.
//
// includeDisabled matters: Slack has no delete, so a "removed" group is merely disabled
// and is invisible without it. Adopt-on-create depends on seeing those.
func (c *Client) ListUserGroups(includeUsers, includeDisabled bool) ([]UserGroup, error) {
	params := map[string]string{
		"include_users":    strconv.FormatBool(includeUsers),
		"include_disabled": strconv.FormatBool(includeDisabled),
		"include_count":    "true",
	}

	req, err := c.newUserGroupRequest("usergroups.list", params)
	if err != nil {
		return nil, err
	}
	body, err := c.doRequest(req)
	if err != nil {
		return nil, err
	}
	res := userGroupListResponse{}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, err
	}
	return res.UserGroups, nil
}

// FindUserGroupByHandle returns the group with the given handle, or nil if none exists.
//
// Disabled groups are included deliberately: their handles stay reserved, so a disabled
// group is exactly what blocks a create and what adopt-on-create needs to find.
func (c *Client) FindUserGroupByHandle(handle string) (*UserGroup, error) {
	groups, err := c.ListUserGroups(true, true)
	if err != nil {
		return nil, err
	}
	for i := range groups {
		if groups[i].Handle == handle {
			return &groups[i], nil
		}
	}
	return nil, nil
}

// DisableUserGroup disables a group via usergroups.disable.
//
// This is the closest Slack offers to deletion. The group persists, and its name and
// handle remain reserved.
func (c *Client) DisableUserGroup(id string) (*UserGroup, error) {
	req, err := c.newUserGroupRequest("usergroups.disable", map[string]string{"usergroup": id})
	if err != nil {
		return nil, err
	}
	return c.decodeUserGroup(req)
}

// EnableUserGroup re-enables a disabled group via usergroups.enable.
func (c *Client) EnableUserGroup(id string) (*UserGroup, error) {
	req, err := c.newUserGroupRequest("usergroups.enable", map[string]string{"usergroup": id})
	if err != nil {
		return nil, err
	}
	return c.decodeUserGroup(req)
}

// UpdateUserGroupUsers replaces the group's entire member list.
//
// Slack offers no add/remove — only replace — which is why the Terraform resource is
// authoritative over membership.
func (c *Client) UpdateUserGroupUsers(id string, userIDs []string) (*UserGroup, error) {
	req, err := c.newUserGroupRequest("usergroups.users.update", map[string]string{
		"usergroup": id,
		"users":     strings.Join(userIDs, ","),
	})
	if err != nil {
		return nil, err
	}
	return c.decodeUserGroup(req)
}

// ListUserGroupUsers returns the group's current member IDs.
func (c *Client) ListUserGroupUsers(id string) ([]string, error) {
	req, err := c.newUserGroupRequest("usergroups.users.list", map[string]string{"usergroup": id})
	if err != nil {
		return nil, err
	}
	body, err := c.doRequest(req)
	if err != nil {
		return nil, err
	}
	res := userGroupUsersResponse{}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, err
	}
	return res.Users, nil
}
