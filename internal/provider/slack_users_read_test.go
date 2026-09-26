package provider

// Tasks 3.4-3.10: the slack_users read pipeline.
//
// Every test here drives Read against the mixed fixture, whose four members are chosen
// to make each filter and each selector distinguishable:
//
//   W012A3CDE spengler  human, active, admin,  email Spengler@Ghostbusters.example.com
//   W07QCRPA4 glinda    human, active, guest,  email glinda@south.oz.example.com
//   B01BOT0001 buildbot bot,   active,         no email
//   W0DEACT001 ghost    human, DELETED,        email ghost@ghostbusters.example.com

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func readUsers(t *testing.T, rt map[string]stub, sel map[string]tftypes.Value) *datasource.ReadResponse {
	t.Helper()
	ctx := context.Background()
	d := &usersDataSource{client: newStubClient(t, rt)}
	cfg := usersConfig(t, sel)
	objType := cfg.Schema.Type().TerraformType(ctx).(tftypes.Object)
	resp := &datasource.ReadResponse{
		State: tfsdk.State{Schema: cfg.Schema, Raw: tftypes.NewValue(objType, nil)},
	}
	d.Read(ctx, datasource.ReadRequest{Config: cfg}, resp)
	return resp
}

// matchedIDs reads the resulting user IDs out of state, sorted for comparison.
func matchedIDs(t *testing.T, resp *datasource.ReadResponse) []string {
	t.Helper()
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	var m usersDataSourceModel
	if d := resp.State.Get(context.Background(), &m); d.HasError() {
		t.Fatalf("reading state: %v", d)
	}

	ids := make([]string, 0, len(m.Users.Elements()))
	for id := range m.Users.Elements() {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	if got := int(m.UserCount.ValueInt64()); got != len(ids) {
		t.Errorf("user_count = %d, want %d", got, len(ids))
	}
	if got := len(m.UserIDs.Elements()); got != len(ids) {
		t.Errorf("user_ids has %d entries, want %d", got, len(ids))
	}
	return ids
}

func equalIDs(t *testing.T, got, want []string) {
	t.Helper()
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("matched %v, want %v", got, want)
	}
}

var mixedWorkspace = map[string]stub{
	"/api/users.list": fixture("users_list_mixed.json"),
}

// --- AC-1: one test per selector ------------------------------------------------

func TestUsersRead_NoSelectorReturnsWholeWorkspace(t *testing.T) {
	resp := readUsers(t, mixedWorkspace, nil)
	equalIDs(t, matchedIDs(t, resp), []string{"W012A3CDE", "W07QCRPA4", "B01BOT0001", "W0DEACT001"})
}

func TestUsersRead_ByUsernames(t *testing.T) {
	resp := readUsers(t, mixedWorkspace, map[string]tftypes.Value{
		"usernames": tfStringSet("spengler", "glinda"),
	})
	equalIDs(t, matchedIDs(t, resp), []string{"W012A3CDE", "W07QCRPA4"})
}

func TestUsersRead_ByEmails(t *testing.T) {
	resp := readUsers(t, mixedWorkspace, map[string]tftypes.Value{
		"emails": tfStringSet("glinda@south.oz.example.com"),
	})
	equalIDs(t, matchedIDs(t, resp), []string{"W07QCRPA4"})
}

// Slack treats email as case-insensitive, and the fixture's stored address is
// deliberately mixed-case. A config that types it in lower case must still match.
func TestUsersRead_EmailsAreCaseInsensitive(t *testing.T) {
	resp := readUsers(t, mixedWorkspace, map[string]tftypes.Value{
		"emails": tfStringSet("spengler@ghostbusters.example.com"),
	})
	equalIDs(t, matchedIDs(t, resp), []string{"W012A3CDE"})
}

// Usernames are the opposite: Slack handles are exact.
func TestUsersRead_UsernamesAreCaseSensitive(t *testing.T) {
	resp := readUsers(t, mixedWorkspace, map[string]tftypes.Value{
		"usernames": tfStringSet("Spengler"),
	})
	if !resp.Diagnostics.HasError() {
		t.Fatal(`"Spengler" must not match the handle "spengler"`)
	}
}

func TestUsersRead_ByChannel(t *testing.T) {
	resp := readUsers(t, map[string]stub{
		"/api/users.list":            fixture("users_list_mixed.json"),
		"/api/conversations.members": fixture("conversations_members_ok.json"),
	}, map[string]tftypes.Value{
		"channel": tftypes.NewValue(tftypes.String, "C012AB3CD"),
	})
	// The channel fixture holds spengler, glinda and the bot -- not the deactivated one.
	equalIDs(t, matchedIDs(t, resp), []string{"W012A3CDE", "W07QCRPA4", "B01BOT0001"})
}

// The whole user object must come through, not just the ID -- that is the point of
// returning objects rather than a set of strings.
func TestUsersRead_CarriesTheFullUserObject(t *testing.T) {
	resp := readUsers(t, mixedWorkspace, map[string]tftypes.Value{
		"usernames": tfStringSet("spengler"),
	})
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	var m usersDataSourceModel
	if d := resp.State.Get(context.Background(), &m); d.HasError() {
		t.Fatalf("reading state: %v", d)
	}

	elem, ok := m.Users.Elements()["W012A3CDE"]
	if !ok {
		t.Fatal("spengler is missing from users")
	}
	if !strings.Contains(elem.String(), "Paranormal Investigator") {
		t.Errorf("the nested profile did not come through: %s", elem.String())
	}
}
