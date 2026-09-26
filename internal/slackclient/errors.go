package slackclient

// Story: Slack application errors
//
// Input:  the "error" field from a Slack Web API response body that carried ok:false.
// Process:
//   1. Wrap the code in a typed error so callers can branch on it with errors.As.
//   2. Record which endpoint produced it, for actionable diagnostics.
// Output: an error value that behaves like any other error but carries Slack's code.
//
// Dependencies: none beyond the standard library.
// Side effects: none.
//
// Why a typed error rather than fmt.Errorf: messageResource.Read distinguishes
// "thread_not_found" (the message was genuinely deleted -- drop it from state) from a
// transport failure (Slack was unreachable -- fail loudly). Collapsing both into an
// opaque error string is exactly the A-4 defect. The new slack_user data source needs
// the same distinction between users_not_found and missing_scope.

import (
	"errors"
	"fmt"
)

// SlackError is returned when the Slack Web API answers HTTP 200 with ok:false.
//
// Slack signals application-level failures -- invalid auth, missing scope, unknown
// user, rate limiting -- in the response body rather than the HTTP status. Callers
// should branch on Code, not on the message text.
type SlackError struct {
	// Code is Slack's "error" field, e.g. "users_not_found" or "missing_scope".
	Code string
	// Endpoint is the API method that produced the error, e.g. "users.info".
	Endpoint string
}

func (e *SlackError) Error() string {
	if e.Endpoint != "" {
		return fmt.Sprintf("slack api error %q from %s", e.Code, e.Endpoint)
	}
	return fmt.Sprintf("slack api error %q", e.Code)
}

// ErrorCode returns the Slack error code carried by err, unwrapping as needed.
// It returns "" when err is nil or is not a *SlackError, so callers can compare
// against a code without first checking the type.
func ErrorCode(err error) string {
	var se *SlackError
	if errors.As(err, &se) {
		return se.Code
	}
	return ""
}
