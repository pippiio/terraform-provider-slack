package provider

// Story: Slack error classification
//
// Input:  an error from any slackclient call.
// Process: map Slack's error code onto the small set of categories a caller has to
//          distinguish in order to say something useful about it.
// Output: a slackErrorKind.
//
// Dependencies: slackclient.ErrorCode.
// Side effects: none -- pure function.
//
// FR-12 asked for the "lookup-independent" branches of lookupErrorDiagnostic to be
// shared with slack_users. Once both callers existed it became clear that is the wrong
// seam: almost every branch embeds what the caller was doing ("while looking up the
// email address x", "Reduce the number of slack_user data sources"), and a shared
// function would either lose that or take the whole sentence as an argument, which
// shares nothing.
//
// What genuinely must not drift is the *classification* -- which codes mean the token
// was rejected, which mean rate limiting. If one data source learns about a new
// auth-failure code and the other does not, the same broken token yields an actionable
// message from one and a shrug from the other. So the categories are shared and the
// prose stays local, which is the opposite split from the one FR-12 described.

import "terraform-provider-slack/internal/slackclient"

type slackErrorKind int

const (
	// slackErrorOther is anything unrecognised, including a non-Slack error such as a
	// transport failure. Callers must treat it as fatal, never as "probably fine".
	slackErrorOther slackErrorKind = iota
	// slackErrorAuth means Slack rejected the token itself.
	slackErrorAuth
	// slackErrorScope means the token is valid but lacks a required scope.
	slackErrorScope
	// slackErrorRateLimited means Slack throttled the call. The provider does not retry.
	slackErrorRateLimited
	// slackErrorNotFound means the thing asked for does not exist, or is not visible to
	// this token -- Slack does not distinguish those.
	slackErrorNotFound
)

// authCodes are the codes that mean "this token will not work until it is replaced".
// Kept as a set rather than a switch case so the list is one edit in one place.
var authCodes = map[string]bool{
	"invalid_auth":     true,
	"not_authed":       true,
	"token_revoked":    true,
	"account_inactive": true,
}

// notFoundCodes are the codes that mean the referenced object was not returned. They do
// not distinguish "does not exist" from "you cannot see it", which is why the callers'
// details have to enumerate the possibilities.
var notFoundCodes = map[string]bool{
	"users_not_found":   true,
	"channel_not_found": true,
	"no_such_subteam":   true,
}

func classifySlackError(err error) slackErrorKind {
	code := slackclient.ErrorCode(err)

	switch {
	case authCodes[code]:
		return slackErrorAuth
	case code == "missing_scope":
		return slackErrorScope
	case code == "ratelimited":
		return slackErrorRateLimited
	case notFoundCodes[code]:
		return slackErrorNotFound
	default:
		return slackErrorOther
	}
}
