package provider

// Task 4.2b / FR-12. Two data sources now translate Slack error codes into diagnostics.
// The prose must differ -- an actionable message names what the caller was doing -- but
// the *classification* must not. If one of them learns about a new auth-failure code and
// the other does not, the same broken token produces a helpful message from one data
// source and "reading the Slack user list failed" from the other.

import (
	"strings"
	"testing"

	"terraform-provider-slack/internal/slackclient"
)

func slackErr(code string) error { return &slackclient.SlackError{Code: code, Endpoint: "users.list"} }

func TestClassifySlackError(t *testing.T) {
	for code, want := range map[string]slackErrorKind{
		"invalid_auth":      slackErrorAuth,
		"not_authed":        slackErrorAuth,
		"token_revoked":     slackErrorAuth,
		"account_inactive":  slackErrorAuth,
		"missing_scope":     slackErrorScope,
		"ratelimited":       slackErrorRateLimited,
		"users_not_found":   slackErrorNotFound,
		"channel_not_found": slackErrorNotFound,
		"something_else":    slackErrorOther,
	} {
		if got := classifySlackError(slackErr(code)); got != want {
			t.Errorf("classifySlackError(%q) = %v, want %v", code, got, want)
		}
	}
}

// The drift guard: every code either data source treats as an auth failure must be
// treated as one by both.
func TestBothDataSourcesAgreeOnAuthFailures(t *testing.T) {
	for _, code := range []string{"invalid_auth", "not_authed", "token_revoked", "account_inactive"} {
		err := slackErr(code)

		userSummary, _ := lookupErrorDiagnostic(err, lookupByID, "W012A3CDE")
		usersSummary, _ := usersScanErrorDiagnostic(err)

		if !strings.Contains(strings.ToLower(userSummary), "token") {
			t.Errorf("slack_user does not report %q as a token failure: %q", code, userSummary)
		}
		if !strings.Contains(strings.ToLower(usersSummary), "token") {
			t.Errorf("slack_users does not report %q as a token failure: %q", code, usersSummary)
		}
	}
}

// Same for rate limiting: both must name it, and both must say the provider does not
// retry, because the operator's next action is the same either way.
func TestBothDataSourcesAgreeOnRateLimiting(t *testing.T) {
	err := slackErr("ratelimited")

	_, userDetail := lookupErrorDiagnostic(err, lookupByID, "W012A3CDE")
	_, usersDetail := usersScanErrorDiagnostic(err)

	for name, detail := range map[string]string{"slack_user": userDetail, "slack_users": usersDetail} {
		if !strings.Contains(detail, "does not retry") {
			t.Errorf("%s must say the provider does not retry: %s", name, detail)
		}
	}
}
