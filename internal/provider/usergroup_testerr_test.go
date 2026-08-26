package provider

import "terraform-provider-slack/internal/slackclient"

// testSlackErr wraps a code as a *slackclient.SlackError for diagnostic tests.
type testSlackErr struct{ code string }

func (e *testSlackErr) Error() string { return "slack api error " + e.code }

func (e *testSlackErr) Unwrap() error {
	return &slackclient.SlackError{Code: e.code, Endpoint: "usergroups.test"}
}
