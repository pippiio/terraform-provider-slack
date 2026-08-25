package provider

// Story: read-time drift classification
//
// Input:  the error (possibly nil) from a per-entry ReadMessage call.
// Process:
//   1. No error -> the message is still there, keep the entry.
//   2. A Slack error whose code positively means the message no longer exists ->
//      drop the entry, so Terraform sees the drift and can recreate.
//   3. Anything else -- transport failure, auth failure, rate limiting, or an
//      ambiguous Slack code -> fail loudly. Do not touch state.
// Output: a readOutcome telling Read what to do with the entry.
//
// Dependencies: slackclient.ErrorCode.
// Side effects: none -- pure function.
//
// Why this exists: Read previously used `if err != nil || apiresp.Err ==
// "thread_not_found" { continue }`, which treats a network outage exactly like a
// deleted message. That silently discards state for messages that still exist, and
// Terraform then recreates them -- posting duplicates. Guardrail A-4.
//
// The default is deliberately fail-closed: an outcome we cannot positively identify
// as "the message is gone" must never destroy state.

import "terraform-provider-slack/internal/slackclient"

// readOutcome is what Read should do with a single msg_map entry.
type readOutcome int

const (
	// readOutcomeKeep means the message was read successfully.
	readOutcomeKeep readOutcome = iota
	// readOutcomeDrop means the message is confirmed gone; remove it from state.
	readOutcomeDrop
	// readOutcomeFail means we could not determine the message's fate; surface a
	// diagnostic and leave state alone.
	readOutcomeFail
)

func (o readOutcome) String() string {
	switch o {
	case readOutcomeKeep:
		return "keep"
	case readOutcomeDrop:
		return "drop"
	case readOutcomeFail:
		return "fail"
	default:
		return "unknown"
	}
}

// messageGoneCodes are the Slack error codes that positively confirm a message no
// longer exists. Codes are added here only when they cannot also be produced by a
// recoverable condition -- channel_not_found, for instance, is excluded because it is
// also returned when the bot has merely been removed from the channel.
var messageGoneCodes = map[string]bool{
	"thread_not_found":  true,
	"message_not_found": true,
}

// classifyReadError decides what a per-message read error means for stored state.
func classifyReadError(err error) readOutcome {
	if err == nil {
		return readOutcomeKeep
	}
	if messageGoneCodes[slackclient.ErrorCode(err)] {
		return readOutcomeDrop
	}
	return readOutcomeFail
}
