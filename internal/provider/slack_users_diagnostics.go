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

// emailScope is the scope Slack requires before it will return profile.email at all.
const emailScope = "users:read.email"

// scopeInferredFromBehaviour is the sentence the FR-9 fallback adds when the token's
// scopes are unknown and no scanned user carried an email. It names the observation, not
// just the conclusion, so an operator can judge the inference rather than trust it.
const scopeInferredFromBehaviour = "Note that none of the users Slack returned had an " +
	"email address at all, which usually means the token is missing the `" + emailScope +
	"` scope -- without it Slack omits the field entirely and nothing can ever match."

// usersScanErrorDiagnostic reports a failure during the users.list walk.
//
// A partial scan is never turned into a partial result: the whole read fails. The
// difference between an error and a silently shrunken set is the failure mode this
// provider keeps rediscovering, and slack_users feeds sets that other resources treat
// as authoritative.
func usersScanErrorDiagnostic(err error) (string, string) {
	switch classifySlackError(err) {
	case slackErrorScope:
		return "Slack token is missing a required scope", fmt.Sprintf(
			"Reading the workspace user list requires the %q scope, which this token does "+
				"not have.\n\nAdd the scope to your Slack app, reinstall it to the workspace, "+
				"and use the regenerated token.\n\nUnderlying error: %s",
			"users:read", err,
		)

	case slackErrorAuth:
		return "Slack rejected the API token", fmt.Sprintf(
			"Slack rejected the configured token while listing users.\n\nCheck the `token` "+
				"provider attribute or the SLACK_TOKEN environment variable.\n\n"+
				"Underlying error: %s", err,
		)

	case slackErrorRateLimited:
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
// FR-10: Slack answers channel_not_found for three unrelated conditions, and two of them
// are fixed somewhere other than the channel ID. Listing all three is the whole point --
// an operator who reads only the code goes and checks the ID, which is the one cause they
// can already see is correct.
func channelErrorDiagnostic(err error, channel string) (string, string) {
	if slackclient.ErrorCode(err) == "channel_not_found" {
		return "Slack channel not found", fmt.Sprintf(
			"Slack returned `channel_not_found` for the channel %q. Slack uses that one code "+
				"for three different situations, and does not say which applies:\n\n"+
				"  1. No channel has that ID. Note this attribute takes an ID (`C…`), not a "+
				"name — `#general` will never match.\n"+
				"  2. It is a private channel and the token lacks the `groups:read` scope. "+
				"`channels:read` only covers public channels.\n"+
				"  3. It is a private channel the token was never invited to. Scopes do not "+
				"substitute for membership; invite the app to the channel.\n\n"+
				"Underlying error: %s",
			channel, err,
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
func unresolvedInputsDiagnostic(missing []string, config usersDataSourceModel, sawAnyEmail bool) (string, string) {
	what := "usernames"
	hint := "Usernames are Slack handles -- the `name` field, not the display name -- and " +
		"are matched exactly and case-sensitively."

	if !config.Emails.IsNull() {
		what = "email addresses"
		hint = "Email matching is case-insensitive."

		// FR-9's behavioural fallback, reached only when Slack did not report the
		// token's scopes. Every user lacking an email is not proof, but it is a far
		// better first place to look than the addresses themselves -- and adding it only
		// when it is actually observed keeps it from becoming noise on every miss.
		if !sawAnyEmail {
			hint += " " + scopeInferredFromBehaviour
		}
	}

	return "Unresolved Slack " + what, fmt.Sprintf(
		"No Slack user matched these %s:\n  %s\n\n%s\n\nSet `error_on_no_match = false` to "+
			"tolerate inputs that resolve to nothing.",
		what, strings.Join(missing, "\n  "), hint,
	)
}

// missingEmailScopeDiagnostic reports a token Slack has positively said does not hold
// users:read.email, for a config whose selector depends on it.
func missingEmailScopeDiagnostic() (string, string) {
	return "Slack token is missing the " + emailScope + " scope", fmt.Sprintf(
		"The `emails` selector matches against `profile.email`, which Slack omits entirely "+
			"unless the token holds the %q scope. Slack has reported this token's scopes and "+
			"that one is not among them, so no address can ever match.\n\n"+
			"Add the scope to your Slack app, reinstall it to the workspace, and use the "+
			"regenerated token. To look these users up without it, select them by "+
			"`usernames` instead.",
		emailScope,
	)
}
