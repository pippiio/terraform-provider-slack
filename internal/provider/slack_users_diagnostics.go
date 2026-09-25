package provider

// Story: slack_users diagnostics
//
// Input:  an error from the scan or the container lookup, or a list of inputs that
//         matched nothing.
// Process: translate into a summary and a detail that says what to actually change.
// Output: a (summary, detail) pair for resp.Diagnostics.
//
// Dependencies: slackclient.ErrorCode.
// Side effects: none -- pure functions.
//
// These start deliberately thin. FR-10's three-causes detail for channel_not_found and
// FR-9's scope-aware email message are driven by their own tests in Phase 4; writing
// that wording here first would be production code ahead of the test that specifies it.

import (
	"fmt"
	"strings"

	"terraform-provider-slack/internal/slackclient"
)

// usersScanErrorDiagnostic reports a failure during the users.list walk.
//
// A partial scan is never turned into a partial result: the whole read fails. The
// difference between an error and a silently shrunken set is the failure mode this
// provider keeps rediscovering, and slack_users feeds sets that other resources treat
// as authoritative.
func usersScanErrorDiagnostic(err error) (string, string) {
	switch slackclient.ErrorCode(err) {
	case "missing_scope":
		return "Slack token is missing a required scope", fmt.Sprintf(
			"Reading the workspace user list requires the %q scope, which this token does "+
				"not have.\n\nAdd the scope to your Slack app, reinstall it to the workspace, "+
				"and use the regenerated token.\n\nUnderlying error: %s",
			"users:read", err,
		)

	case "invalid_auth", "not_authed", "token_revoked", "account_inactive":
		return "Slack rejected the API token", fmt.Sprintf(
			"Slack rejected the configured token while listing users.\n\nCheck the `token` "+
				"provider attribute or the SLACK_TOKEN environment variable.\n\n"+
				"Underlying error: %s", err,
		)

	case "ratelimited":
		return "Slack rate limit reached", fmt.Sprintf(
			"Slack rate-limited the user list part-way through. The provider does not retry, "+
				"and will not return a partial result: a half-read workspace would look like "+
				"users who no longer exist.\n\nRe-run the apply, or reduce how many Slack data "+
				"sources resolve at once.\n\nUnderlying error: %s", err,
		)

	default:
		return "Unable to list Slack users", fmt.Sprintf("Reading the Slack user list failed: %s", err)
	}
}

// channelErrorDiagnostic reports a failure resolving the channel selector.
//
// FR-10 requires this to name all three conditions Slack collapses into
// channel_not_found; that wording arrives with its test in Task 4.1.
func channelErrorDiagnostic(err error, channel string) (string, string) {
	if slackclient.ErrorCode(err) == "channel_not_found" {
		return "Slack channel not found", fmt.Sprintf(
			"Slack returned channel_not_found for %q.", channel,
		)
	}
	return "Unable to read Slack channel members", fmt.Sprintf(
		"Reading the members of channel %q failed: %s", channel, err,
	)
}

// unresolvedInputsDiagnostic reports inputs that matched no Slack account.
//
// It lists only the failures. Naming everything that was asked for would bury the one
// line the operator has to act on.
func unresolvedInputsDiagnostic(missing []string, config usersDataSourceModel) (string, string) {
	what := "usernames"
	hint := "Usernames are Slack handles -- the `name` field, not the display name -- and " +
		"are matched exactly and case-sensitively."
	if !config.Emails.IsNull() {
		what = "email addresses"
		hint = "Email matching is case-insensitive. Note that it needs the " +
			"`users:read.email` scope: without it Slack omits the field entirely and " +
			"nothing can match."
	}

	return "Unresolved Slack " + what, fmt.Sprintf(
		"No Slack user matched these %s:\n  %s\n\n%s\n\nSet `error_on_no_match = false` to "+
			"tolerate inputs that resolve to nothing.",
		what, strings.Join(missing, "\n  "), hint,
	)
}
