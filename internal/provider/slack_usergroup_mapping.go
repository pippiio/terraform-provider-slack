package provider

// Story: user group <-> Terraform mapping and diagnostics
//
// Input:  a *slackclient.UserGroup, or an error from a usergroups.* call.
// Process:
//   1. Map the group onto the schema, preserving null for values Slack omitted.
//   2. Carry prior state forward where Slack did not return a value, so an absent field
//      never blanks a managed one and produces a phantom diff.
//   3. Translate Slack error codes into diagnostics that say what to actually do.
// Output: a populated model, or a (summary, detail) pair.
//
// Side effects: none -- pure functions.

import (
	"context"
	"fmt"

	"terraform-provider-slack/internal/slackclient"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// setToStrings converts a types.Set of strings to a Go slice. A null or unknown set
// yields nil, which callers read as "not managed".
func setToStrings(ctx context.Context, s types.Set, diags *diag.Diagnostics) []string {
	if s.IsNull() || s.IsUnknown() {
		return nil
	}
	var out []string
	diags.Append(s.ElementsAs(ctx, &out, false)...)
	return out
}

// stringsToSet converts a slice to a types.Set, rendering nil as null rather than an
// empty set — the two mean different things here.
func stringsToSet(ctx context.Context, in []string, diags *diag.Diagnostics) types.Set {
	if in == nil {
		return types.SetNull(types.StringType)
	}
	s, d := types.SetValueFrom(ctx, types.StringType, in)
	diags.Append(d...)
	return s
}

// userGroupToModel maps a Slack group onto the schema.
//
// prior is the previous model (plan or state). It matters for two fields:
//
//   - channels: if Slack omits prefs entirely, the stored value is carried forward
//     rather than blanked. Blanking would produce a phantom diff on every plan (FR-9a).
//   - users: if the configuration never set `users`, it stays null. Populating it from
//     the API would silently start managing membership the operator chose to leave alone
//     (FR-8a).
func userGroupToModel(ctx context.Context, g *slackclient.UserGroup, prior userGroupResourceModel) (userGroupResourceModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	channels := prior.Channels
	if g.Prefs != nil {
		channels = stringsToSet(ctx, g.Prefs.Channels, &diags)
	}

	users := prior.Users
	if !prior.Users.IsNull() && g.Users != nil {
		users = stringsToSet(ctx, g.Users, &diags)
	}

	description := prior.Description
	if g.Description != nil {
		description = types.StringValue(*g.Description)
	}

	return userGroupResourceModel{
		ID:          types.StringValue(g.ID),
		Name:        types.StringValue(g.Name),
		Handle:      types.StringValue(g.Handle),
		Description: description,
		Channels:    channels,
		Users:       users,

		TeamID:             types.StringValue(g.TeamID),
		UserCount:          types.Int64Value(g.UserCount.Int64()),
		DateCreate:         types.Int64Value(derefInt64(g.DateCreate)),
		DateUpdate:         types.Int64Value(derefInt64(g.DateUpdate)),
		IsDisabled:         types.BoolValue(g.IsDisabled()),
		IsIDPGroup:         types.BoolValue(derefBool(g.IsIDPGroup)),
		IsMembershipLocked: types.BoolValue(derefBool(g.IsMembershipLocked)),
	}, diags
}

func derefInt64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func derefBool(p *bool) bool {
	return p != nil && *p
}

// userGroupErrorDiagnostic turns a usergroups.* failure into an actionable message.
//
// action reads naturally in a sentence ("create", "update", "disable"), and identifier is
// the handle or ID involved.
func userGroupErrorDiagnostic(err error, action, identifier string) (string, string) {
	switch slackclient.ErrorCode(err) {
	case "paid_only":
		return "Slack user groups require a paid plan", fmt.Sprintf(
			"Slack refused to %s the user group %q because user groups are only available on "+
				"paid Slack plans.\n\nThis workspace appears to be on the free plan, where every "+
				"usergroups API call fails this way. The slack_usergroup resource cannot be used "+
				"here.\n\nUnderlying error: %s",
			action, identifier, err,
		)

	case "permission_denied":
		return "Slack refused the user group operation", fmt.Sprintf(
			"Slack denied permission to %s the user group %q.\n\n"+
				"This is usually not a missing OAuth scope: Slack workspaces can restrict who may "+
				"create or edit user groups, and the app inherits that restriction. Ask a workspace "+
				"admin to allow the app to manage user groups, or check the workspace's user group "+
				"permission settings.\n\nUnderlying error: %s",
			action, identifier, err,
		)

	case "missing_scope":
		return "Slack token is missing a required scope", fmt.Sprintf(
			"Managing user groups requires the `usergroups:read` and `usergroups:write` scopes, "+
				"which this token does not have.\n\nAdd both to your Slack app, reinstall it to the "+
				"workspace, and use the regenerated token.\n\nUnderlying error: %s",
			err,
		)

	case "name_already_exists", "handle_already_exists", "subteam_handle_already_exists":
		return "Slack user group name or handle is already in use", fmt.Sprintf(
			"Slack rejected the %s of user group %q because its name or handle is already taken.\n\n"+
				"Note that Slack never deletes user groups: a disabled group keeps its name and handle "+
				"reserved. If you previously destroyed this group, run `terraform import` to bring the "+
				"existing one back under management, or choose a different handle.\n\n"+
				"Underlying error: %s",
			action, identifier, err,
		)

	case "no_such_subteam":
		return "Slack user group not found", fmt.Sprintf(
			"No user group matches %q. It may have been removed from the workspace, or the ID may "+
				"be wrong.\n\nUnderlying error: %s",
			identifier, err,
		)

	case "invalid_users":
		return "Slack rejected one or more user IDs", fmt.Sprintf(
			"Slack rejected the member list for user group %q. Every entry must be a Slack user ID "+
				"(for example `U012ABCDE`), not a username or email address. Use the `slack_user` data "+
				"source to resolve people to IDs.\n\nUnderlying error: %s",
			identifier, err,
		)

	case "invalid_auth", "not_authed", "token_revoked", "account_inactive":
		return "Slack rejected the API token", fmt.Sprintf(
			"Slack rejected the configured token while trying to %s user group %q.\n\n"+
				"Check the `token` provider attribute or the SLACK_TOKEN environment variable.\n\n"+
				"Underlying error: %s",
			action, identifier, err,
		)

	case "ratelimited":
		return "Slack rate limit reached", fmt.Sprintf(
			"Slack rate-limited the request to %s user group %q. The provider does not retry; "+
				"re-run the apply.\n\nUnderlying error: %s",
			action, identifier, err,
		)

	default:
		return "Unable to " + action + " Slack user group", fmt.Sprintf(
			"Slack returned an error while trying to %s user group %q: %s", action, identifier, err,
		)
	}
}

// userGroupToDataSourceModel maps a group onto the read-only data source schema.
// Unlike the resource mapping there is no prior state to carry forward: everything the
// data source reports comes from this response.
func userGroupToDataSourceModel(ctx context.Context, g *slackclient.UserGroup) (userGroupDataSourceModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	var channels types.Set
	if g.Prefs != nil {
		channels = stringsToSet(ctx, g.Prefs.Channels, &diags)
	} else {
		channels = types.SetNull(types.StringType)
	}

	description := types.StringNull()
	if g.Description != nil {
		description = types.StringValue(*g.Description)
	}

	return userGroupDataSourceModel{
		ID:          types.StringValue(g.ID),
		Handle:      types.StringValue(g.Handle),
		Name:        types.StringValue(g.Name),
		Description: description,
		Channels:    channels,
		Users:       stringsToSet(ctx, g.Users, &diags),

		TeamID:             types.StringValue(g.TeamID),
		UserCount:          types.Int64Value(g.UserCount.Int64()),
		DateCreate:         types.Int64Value(derefInt64(g.DateCreate)),
		DateUpdate:         types.Int64Value(derefInt64(g.DateUpdate)),
		IsDisabled:         types.BoolValue(g.IsDisabled()),
		IsIDPGroup:         types.BoolValue(derefBool(g.IsIDPGroup)),
		IsMembershipLocked: types.BoolValue(derefBool(g.IsMembershipLocked)),
		IsExternal:         types.BoolValue(derefBool(g.IsExternal)),
	}, diags
}
