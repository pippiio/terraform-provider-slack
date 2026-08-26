package provider

import (
	"errors"
	"testing"

	"terraform-provider-slack/internal/slackclient"
)

// These cover the A-4 distinction: a message that is genuinely gone must be dropped
// from state, but a failure to reach Slack must NOT be -- silently dropping state on a
// transient outage destroys the record of real messages and causes them to be recreated.

func TestClassifyReadError_NilKeepsEntry(t *testing.T) {
	if got := classifyReadError(nil); got != readOutcomeKeep {
		t.Errorf("classifyReadError(nil) = %v, want keep", got)
	}
}

func TestClassifyReadError_ThreadNotFoundDropsEntry(t *testing.T) {
	err := &slackclient.SlackError{Code: "thread_not_found", Endpoint: "conversations.replies"}
	if got := classifyReadError(err); got != readOutcomeDrop {
		t.Errorf("classifyReadError(thread_not_found) = %v, want drop", got)
	}
}

func TestClassifyReadError_MessageNotFoundDropsEntry(t *testing.T) {
	err := &slackclient.SlackError{Code: "message_not_found"}
	if got := classifyReadError(err); got != readOutcomeDrop {
		t.Errorf("classifyReadError(message_not_found) = %v, want drop", got)
	}
}

// The core A-4 regression guard.
func TestClassifyReadError_TransportErrorFailsLoudly(t *testing.T) {
	err := errors.New("dial tcp: connection refused")
	if got := classifyReadError(err); got != readOutcomeFail {
		t.Errorf("classifyReadError(transport) = %v, want fail -- a network outage must not drop state", got)
	}
}

// Ambiguous Slack errors must fail closed rather than destroy state. channel_not_found
// can mean the bot was removed from the channel, not that the message is gone.
func TestClassifyReadError_OtherSlackErrorsFailClosed(t *testing.T) {
	for _, code := range []string{"invalid_auth", "missing_scope", "ratelimited", "channel_not_found"} {
		t.Run(code, func(t *testing.T) {
			err := &slackclient.SlackError{Code: code}
			if got := classifyReadError(err); got != readOutcomeFail {
				t.Errorf("classifyReadError(%s) = %v, want fail", code, got)
			}
		})
	}
}
