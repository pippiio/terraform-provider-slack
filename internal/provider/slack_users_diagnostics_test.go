package provider

// Tasks 4.1-4.2: the two diagnostics Slack's own error codes cannot express.

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// --- Task 4.1 / FR-10, AC-7 -------------------------------------------------------
//
// Slack answers channel_not_found for three unrelated conditions. An operator reading
// the raw code cannot tell which one they hit, and two of the three are fixed somewhere
// other than the channel ID.

func TestUsersRead_ChannelNotFoundNamesAllThreeCauses(t *testing.T) {
	resp := readUsers(t, map[string]stub{
		"/api/users.list":            fixture("users_list_mixed.json"),
		"/api/conversations.members": fixture("err_channel_not_found.json"),
	}, map[string]tftypes.Value{
		"channel": tftypes.NewValue(tftypes.String, "C000000000"),
	})

	detail := strings.ToLower(readErr(t, resp))

	if !strings.Contains(detail, "c000000000") {
		t.Error("the detail must name the channel that failed")
	}
	// Three enumerated causes, not a wall of prose an operator has to parse.
	for _, n := range []string{"1.", "2.", "3."} {
		if !strings.Contains(detail, n) {
			t.Errorf("must enumerate three distinct causes; %q is missing: %s", n, detail)
		}
	}
	// Cause 1: the ID is wrong -- including the ID-vs-name mistake, which is the one
	// this data source invites by taking an ID where people expect #channel-name.
	if !strings.Contains(detail, "not a name") && !strings.Contains(detail, "#general") {
		t.Errorf("must warn that this takes an ID rather than a channel name: %s", detail)
	}
	// Cause 2: a private channel the token cannot see without groups:read.
	if !strings.Contains(detail, "groups:read") {
		t.Errorf("must name groups:read for the private-channel case: %s", detail)
	}
	// Cause 3: the token is not in the channel.
	if !strings.Contains(detail, "member") && !strings.Contains(detail, "invited") {
		t.Errorf("must offer 'the token is not in the channel' as a cause: %s", detail)
	}
}

// scopeInferredMarker is the sentence the behavioural fallback adds, and only it. Tests
// assert on this rather than on "users:read.email", which the generic email hint names
// in every case and so cannot tell the two paths apart.
const scopeInferredMarker = "none of the users Slack returned had an email address at all"

// --- Task 4.2 / FR-9, AC-8 --------------------------------------------------------

// Authoritative path: Slack reported the scopes, users:read.email is not among them, so
// nothing can possibly match. Fail on the spot rather than reading the whole workspace
// to discover it.
func TestUsersRead_EmailsWithoutScopeFailsBeforeScanningOn(t *testing.T) {
	ctx := context.Background()
	c, rec := newRecordingStubClient(t, map[string]stub{
		"/api/users.list": sequence(
			withHeaders(fixture("users_list_page1.json"), map[string]string{"X-OAuth-Scopes": "users:read,chat:write"}),
			withHeaders(fixture("users_list_page2.json"), map[string]string{"X-OAuth-Scopes": "users:read,chat:write"}),
		),
	})
	d := &usersDataSource{client: c}

	cfg := usersConfig(t, map[string]tftypes.Value{"emails": tfStringSet("spengler@ghostbusters.example.com")})
	objType := cfg.Schema.Type().TerraformType(ctx).(tftypes.Object)
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: cfg.Schema, Raw: tftypes.NewValue(objType, nil)}}
	d.Read(ctx, datasource.ReadRequest{Config: cfg}, resp)

	detail := readErr(t, resp)
	if !strings.Contains(detail, "users:read.email") {
		t.Errorf("the diagnostic must name the missing scope: %s", detail)
	}
	if n := rec.countPath("/api/users.list"); n != 1 {
		t.Errorf("made %d users.list requests, want 1 -- every further page is wasted once "+
			"the token is known to lack the scope", n)
	}
}

// The same token, but the emails selector is not in play: the scan must proceed. A
// missing email scope is only fatal to a lookup that depends on emails.
func TestUsersRead_MissingEmailScopeDoesNotBlockOtherSelectors(t *testing.T) {
	resp := readUsers(t, map[string]stub{
		"/api/users.list": withHeaders(
			fixture("users_list_mixed.json"),
			map[string]string{"X-OAuth-Scopes": "users:read"},
		),
	}, map[string]tftypes.Value{"usernames": tfStringSet("spengler")})

	equalIDs(t, matchedIDs(t, resp), []string{"W012A3CDE"})
}

// Scopes granted: the read proceeds normally even though the selector is emails.
func TestUsersRead_EmailsWithScopeProceeds(t *testing.T) {
	resp := readUsers(t, map[string]stub{
		"/api/users.list": withHeaders(
			fixture("users_list_mixed.json"),
			map[string]string{"X-OAuth-Scopes": "users:read,users:read.email"},
		),
	}, map[string]tftypes.Value{"emails": tfStringSet("glinda@south.oz.example.com")})

	equalIDs(t, matchedIDs(t, resp), []string{"W07QCRPA4"})
}

// Fallback path: Slack sent no scope header, so the provider cannot know. It must not
// conclude the scope is missing from silence -- but when not one scanned user carried an
// email, that is strong enough to say so as a likely cause.
func TestUsersRead_EmailsWithNoScopeHeaderFallsBackToBehaviour(t *testing.T) {
	resp := readUsers(t, map[string]stub{
		// No X-OAuth-Scopes, and no member in this fixture has profile.email.
		"/api/users.list": fixture("users_list_ok.json"),
	}, map[string]tftypes.Value{"emails": tfStringSet("spengler@ghostbusters.example.com")})

	detail := readErr(t, resp)
	if !strings.Contains(detail, "users:read.email") {
		t.Errorf("with no user carrying an email at all, the scope is the likely cause "+
			"and the diagnostic must say so: %s", detail)
	}
	// The marker distinguishes "we inferred this from behaviour" from the generic hint
	// that mentions the scope for every email lookup. Without it, this test would pass
	// against an implementation that simply always names the scope.
	if !strings.Contains(detail, scopeInferredMarker) {
		t.Errorf("must state the observation the inference rests on: %s", detail)
	}
}

// The fallback must not fire when emails clearly are visible -- that is an ordinary
// unresolved input, and blaming the scope would send the operator somewhere useless.
func TestUsersRead_UnresolvedEmailIsNotBlamedOnScopeWhenEmailsAreVisible(t *testing.T) {
	resp := readUsers(t, mixedWorkspace, map[string]tftypes.Value{
		"emails": tfStringSet("nobody@example.com"),
	})

	detail := readErr(t, resp)
	if !strings.Contains(detail, "nobody@example.com") {
		t.Errorf("must name the unresolved address: %s", detail)
	}
	if strings.Contains(detail, scopeInferredMarker) {
		t.Errorf("emails are plainly visible here, so the scope inference must not fire: %s", detail)
	}
}
