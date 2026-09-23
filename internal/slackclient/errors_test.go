package slackclient

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// TestSlackError_CarriesCode proves the typed error preserves Slack's error code, which
// is what lets callers distinguish thread_not_found (genuine drift) from a transport
// failure -- the distinction architecture.md A-4 says is currently lost.
func TestSlackError_CarriesCode(t *testing.T) {
	err := &SlackError{Code: "users_not_found", Endpoint: "users.info"}

	if err.Code != "users_not_found" {
		t.Errorf("Code = %q, want %q", err.Code, "users_not_found")
	}
	if !strings.Contains(err.Error(), "users_not_found") {
		t.Errorf("Error() = %q, want it to mention the code", err.Error())
	}
	if !strings.Contains(err.Error(), "users.info") {
		t.Errorf("Error() = %q, want it to mention the endpoint", err.Error())
	}
}

// TestSlackError_MatchesWithErrorsAs proves errors.As works through wrapping, which is
// how messageResource.Read and the new data source will branch on the code.
func TestSlackError_MatchesWithErrorsAs(t *testing.T) {
	wrapped := fmt.Errorf("reading message: %w", &SlackError{Code: "thread_not_found"})

	var se *SlackError
	if !errors.As(wrapped, &se) {
		t.Fatal("errors.As failed to unwrap a wrapped *SlackError")
	}
	if se.Code != "thread_not_found" {
		t.Errorf("unwrapped Code = %q, want thread_not_found", se.Code)
	}
}

// TestSlackError_IsNotConfusedWithOtherErrors guards the fail-closed direction: a plain
// error must never be mistaken for a Slack application error, or transport failures
// would be treated as drift.
func TestSlackError_IsNotConfusedWithOtherErrors(t *testing.T) {
	var se *SlackError
	if errors.As(errors.New("connection refused"), &se) {
		t.Fatal("errors.As matched a plain error as *SlackError")
	}
}

// TestErrorCode_Helper covers the convenience accessor callers use when they only care
// about the code and not the error type.
func TestErrorCode_Helper(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"slack error", &SlackError{Code: "missing_scope"}, "missing_scope"},
		{"wrapped slack error", fmt.Errorf("x: %w", &SlackError{Code: "ratelimited"}), "ratelimited"},
		{"plain error", errors.New("boom"), ""},
		{"nil error", nil, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ErrorCode(tc.err); got != tc.want {
				t.Errorf("ErrorCode() = %q, want %q", got, tc.want)
			}
		})
	}
}
