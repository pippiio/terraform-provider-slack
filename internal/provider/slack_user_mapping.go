package provider

// Story: Slack user -> Terraform model mapping and error diagnostics
//
// Input:  a *slackclient.User, or the error from a failed lookup.
// Process:
//   1. Convert each optional field, preserving nil as a Terraform null rather than
//      collapsing it to "" or false.
//   2. Mirror profile.email onto the top-level email attribute for convenience.
//   3. For errors, translate Slack's error code into a diagnostic that tells the
//      operator what to actually do about it.
// Output: a populated userDataSourceModel, or a (summary, detail) diagnostic pair.
//
// Dependencies: slackclient.ErrorCode for the typed error code.
// Side effects: none -- pure functions.

import (
	"context"
	"fmt"

	"terraform-provider-slack/internal/slackclient"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// tfString renders a nil pointer as null rather than an empty string, so config can
// tell "Slack omitted this field" from "this field is empty".
func tfString(p *string) types.String {
	if p == nil {
		return types.StringNull()
	}
	return types.StringValue(*p)
}

func tfBool(p *bool) types.Bool {
	if p == nil {
		return types.BoolNull()
	}
	return types.BoolValue(*p)
}

func tfInt64(p *int64) types.Int64 {
	if p == nil {
		return types.Int64Null()
	}
	return types.Int64Value(*p)
}

// userToModel maps a Slack user onto the data source schema.
func userToModel(ctx context.Context, u *slackclient.User) (userDataSourceModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	p := u.Profile
	profileObj, d := types.ObjectValue(profileAttrTypes(), map[string]attr.Value{
		"real_name":               tfString(p.RealName),
		"real_name_normalized":    tfString(p.RealNameNormalized),
		"display_name":            tfString(p.DisplayName),
		"display_name_normalized": tfString(p.DisplayNameNormalized),
		"first_name":              tfString(p.FirstName),
		"last_name":               tfString(p.LastName),
		"email":                   tfString(p.Email),
		"title":                   tfString(p.Title),
		"phone":                   tfString(p.Phone),
		"skype":                   tfString(p.Skype),
		"team":                    tfString(p.Team),
		"status_text":             tfString(p.StatusText),
		"status_emoji":            tfString(p.StatusEmoji),
		"status_expiration":       tfInt64(p.StatusExpiration),
		"avatar_hash":             tfString(p.AvatarHash),
		"image_24":                tfString(p.Image24),
		"image_32":                tfString(p.Image32),
		"image_48":                tfString(p.Image48),
		"image_72":                tfString(p.Image72),
		"image_192":               tfString(p.Image192),
		"image_512":               tfString(p.Image512),
		"image_1024":              tfString(p.Image1024),
		"image_original":          tfString(p.ImageOriginal),
		"is_custom_image":         tfBool(p.IsCustomImage),
		"bot_id":                  tfString(p.BotID),
		"api_app_id":              tfString(p.APIAppID),
	})
	diags.Append(d...)

	return userDataSourceModel{
		ID:    types.StringValue(u.ID),
		Email: tfString(p.Email),

		TeamID:   types.StringValue(u.TeamID),
		Name:     types.StringValue(u.Name),
		RealName: tfString(u.RealName),
		Deleted:  tfBool(u.Deleted),
		Color:    tfString(u.Color),
		TZ:       tfString(u.TZ),
		TZLabel:  tfString(u.TZLabel),
		TZOffset: tfInt64(u.TZOffset),

		IsAdmin:           tfBool(u.IsAdmin),
		IsOwner:           tfBool(u.IsOwner),
		IsPrimaryOwner:    tfBool(u.IsPrimaryOwner),
		IsRestricted:      tfBool(u.IsRestricted),
		IsUltraRestricted: tfBool(u.IsUltraRestricted),
		IsBot:             tfBool(u.IsBot),
		IsAppUser:         tfBool(u.IsAppUser),
		IsEmailConfirmed:  tfBool(u.IsEmailConfirmed),
		Has2FA:            tfBool(u.Has2FA),

		Updated: tfInt64(u.Updated),
		Profile: profileObj,
	}, diags
}

// lookupErrorDiagnostic turns a lookup failure into an actionable message.
//
// The point of naming the specific scope or condition is that Slack's raw error codes
// ("missing_scope") tell an operator nothing about what to change.
func lookupErrorDiagnostic(err error, lookupByID bool, identifier string) (string, string) {
	lookupKind := "email address"
	requiredScope := "users:read.email"
	if lookupByID {
		lookupKind = "Slack user ID"
		requiredScope = "users:read"
	}

	switch slackclient.ErrorCode(err) {
	case "users_not_found":
		detail := fmt.Sprintf("No Slack user was found for the %s %q.", lookupKind, identifier)
		if !lookupByID {
			detail += "\n\nNote that users.lookupByEmail does not match deactivated accounts. " +
				"If the user has been deactivated, look them up by `id` instead."
		}
		return "Slack user not found", detail

	case "missing_scope":
		return "Slack token is missing a required scope", fmt.Sprintf(
			"Looking a user up by %s requires the %q scope, which this token does not have.\n\n"+
				"Add the scope to your Slack app, reinstall it to the workspace, and use the "+
				"regenerated token.\n\nUnderlying error: %s",
			lookupKind, requiredScope, err,
		)

	case "invalid_auth", "not_authed", "token_revoked", "account_inactive":
		return "Slack rejected the API token", fmt.Sprintf(
			"Slack rejected the configured token while looking up the %s %q.\n\n"+
				"Check the `token` provider attribute or the SLACK_TOKEN environment variable.\n\n"+
				"Underlying error: %s",
			lookupKind, identifier, err,
		)

	case "ratelimited":
		return "Slack rate limit reached", fmt.Sprintf(
			"Slack rate-limited the lookup for %q. The provider does not retry.\n\n"+
				"Reduce the number of slack_user data sources resolved in a single apply, or "+
				"re-run the apply.\n\nUnderlying error: %s",
			identifier, err,
		)

	default:
		return "Unable to read Slack user", fmt.Sprintf(
			"Looking up the %s %q failed: %s", lookupKind, identifier, err,
		)
	}
}
