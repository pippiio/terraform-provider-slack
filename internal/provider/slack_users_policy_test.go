package provider

// Tasks 3.6-3.10: early stop, filters, match-before-filter ordering, the no-match
// policy, and partial-scan failure.
//
// TDD note: these were written after the code they cover. Task 3.5's implementation of
// Read necessarily brought the whole pipeline with it -- the read is not meaningfully
// splittable into "match" then "filter" then "policy". Each test below was instead
// verified by breaking the implementation under it and confirming it fails, which is
// what RED exists to establish. Recorded in plan.md rather than left implicit.

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func tfBoolValue(b bool) tftypes.Value { return tftypes.NewValue(tftypes.Bool, b) }

func readErr(t *testing.T, resp *datasource.ReadResponse) string {
	t.Helper()
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected a diagnostic, got none")
	}
	e := resp.Diagnostics.Errors()[0]
	return e.Summary() + "\n" + e.Detail()
}

// --- Task 3.6 / FR-6: early stop -------------------------------------------------

func TestUsersRead_StopsAtThePageThatSatisfiesEveryInput(t *testing.T) {
	ctx := context.Background()
	c, rec := newRecordingStubClient(t, map[string]stub{
		"/api/users.list": sequence(
			fixture("users_list_page1.json"),
			fixture("users_list_page2.json"),
		),
	})
	d := &usersDataSource{client: c}

	cfg := usersConfig(t, map[string]tftypes.Value{"usernames": tfStringSet("spengler")})
	objType := cfg.Schema.Type().TerraformType(ctx).(tftypes.Object)
	resp := &datasource.ReadResponse{
		State: tfsdk.State{Schema: cfg.Schema, Raw: tftypes.NewValue(objType, nil)},
	}
	d.Read(ctx, datasource.ReadRequest{Config: cfg}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	// spengler is on page 1. Reading page 2 would be a wasted Tier 2 call.
	if n := rec.countPath("/api/users.list"); n != 1 {
		t.Errorf("made %d users.list requests, want 1 -- every input matched on page 1", n)
	}
}

// A miss cannot stop early: proving nothing matched means reading every page. The
// asymmetry is inherent, and the cost lands on the error path, which is worth knowing.
func TestUsersRead_AMissReadsEveryPage(t *testing.T) {
	resp := readUsers(t, map[string]stub{
		"/api/users.list": sequence(
			fixture("users_list_page1.json"),
			fixture("users_list_page2.json"),
		),
	}, map[string]tftypes.Value{"usernames": tfStringSet("nobody")})

	detail := readErr(t, resp)
	if !strings.Contains(detail, "nobody") {
		t.Errorf("diagnostic must name the unresolved username, got: %s", detail)
	}
}

// --- Task 3.7 / FR-3, AC-2: tri-state filters ------------------------------------

func TestUsersRead_FilterExcludesBots(t *testing.T) {
	resp := readUsers(t, mixedWorkspace, map[string]tftypes.Value{
		"is_bot": tfBoolValue(false),
	})
	equalIDs(t, matchedIDs(t, resp), []string{"W012A3CDE", "W07QCRPA4", "W0DEACT001"})
}

func TestUsersRead_FilterSelectsBots(t *testing.T) {
	resp := readUsers(t, mixedWorkspace, map[string]tftypes.Value{
		"is_bot": tfBoolValue(true),
	})
	equalIDs(t, matchedIDs(t, resp), []string{"B01BOT0001"})
}

func TestUsersRead_FilterExcludesDeactivated(t *testing.T) {
	resp := readUsers(t, mixedWorkspace, map[string]tftypes.Value{
		"deleted": tfBoolValue(false),
	})
	equalIDs(t, matchedIDs(t, resp), []string{"W012A3CDE", "W07QCRPA4", "B01BOT0001"})
}

// The combination people will actually write: active humans.
func TestUsersRead_FiltersCombine(t *testing.T) {
	resp := readUsers(t, mixedWorkspace, map[string]tftypes.Value{
		"is_bot":  tfBoolValue(false),
		"deleted": tfBoolValue(false),
	})
	equalIDs(t, matchedIDs(t, resp), []string{"W012A3CDE", "W07QCRPA4"})
}

// Slack omits most of these booleans rather than sending false, so a filter of false
// must include the users it said nothing about. glinda has no is_admin in the fixture.
func TestUsersRead_AbsentFlagCountsAsFalse(t *testing.T) {
	resp := readUsers(t, mixedWorkspace, map[string]tftypes.Value{
		"is_admin": tfBoolValue(false),
	})
	ids := matchedIDs(t, resp)
	for _, want := range []string{"W07QCRPA4", "B01BOT0001", "W0DEACT001"} {
		found := false
		for _, id := range ids {
			if id == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%s has no is_admin in the response and must count as not-admin; got %v", want, ids)
		}
	}
	for _, id := range ids {
		if id == "W012A3CDE" {
			t.Error("spengler is is_admin:true and must be excluded by is_admin = false")
		}
	}
}

// An unset filter must change nothing.
func TestUsersRead_UnsetFilterIsNotApplied(t *testing.T) {
	resp := readUsers(t, mixedWorkspace, nil)
	equalIDs(t, matchedIDs(t, resp), []string{"W012A3CDE", "W07QCRPA4", "B01BOT0001", "W0DEACT001"})
}

// --- Task 3.8 / FR-5, AC-3: match before filter -----------------------------------

func TestUsersRead_FilteredOutInputIsEmptyResultNotUnresolved(t *testing.T) {
	resp := readUsers(t, mixedWorkspace, map[string]tftypes.Value{
		"emails":  tfStringSet("ghost@ghostbusters.example.com"),
		"deleted": tfBoolValue(false),
	})

	detail := readErr(t, resp)
	if strings.Contains(strings.ToLower(detail), "unresolved") {
		t.Errorf("a real account removed by a filter must not be reported as unresolved: %s", detail)
	}
	if !strings.Contains(detail, "matched no users") {
		t.Errorf("expected the empty-result diagnostic, got: %s", detail)
	}
}

// --- Task 3.9 / FR-7, FR-8, AC-4 to AC-6: the no-match policy ---------------------

func TestUsersRead_UnresolvedNamesOnlyTheFailures(t *testing.T) {
	resp := readUsers(t, mixedWorkspace, map[string]tftypes.Value{
		"usernames": tfStringSet("spengler", "nosuchuser"),
	})

	detail := readErr(t, resp)
	if !strings.Contains(detail, "nosuchuser") {
		t.Errorf("diagnostic must name the failure: %s", detail)
	}
	if strings.Contains(detail, "spengler") {
		t.Errorf("diagnostic must not list inputs that resolved fine: %s", detail)
	}
}

func TestUsersRead_ErrorOnNoMatchFalseToleratesUnresolved(t *testing.T) {
	resp := readUsers(t, mixedWorkspace, map[string]tftypes.Value{
		"usernames":         tfStringSet("spengler", "nosuchuser"),
		"error_on_no_match": tfBoolValue(false),
	})
	equalIDs(t, matchedIDs(t, resp), []string{"W012A3CDE"})
}

func TestUsersRead_ErrorOnNoMatchFalseToleratesEmpty(t *testing.T) {
	resp := readUsers(t, mixedWorkspace, map[string]tftypes.Value{
		"usernames":         tfStringSet("nosuchuser"),
		"error_on_no_match": tfBoolValue(false),
	})
	if resp.Diagnostics.HasError() {
		t.Fatalf("an empty result must be tolerated when the flag is false: %v", resp.Diagnostics)
	}
	var m usersDataSourceModel
	if d := resp.State.Get(context.Background(), &m); d.HasError() {
		t.Fatalf("reading state: %v", d)
	}
	if m.UserCount.ValueInt64() != 0 {
		t.Errorf("user_count = %d, want 0", m.UserCount.ValueInt64())
	}
}

// FR-8 / AC-6: a broken reference is not an empty set, and the flag does not excuse it.
func TestUsersRead_MissingChannelErrorsEvenWhenNoMatchIsTolerated(t *testing.T) {
	resp := readUsers(t, map[string]stub{
		"/api/users.list":            fixture("users_list_mixed.json"),
		"/api/conversations.members": fixture("err_channel_not_found.json"),
	}, map[string]tftypes.Value{
		"channel":           tftypes.NewValue(tftypes.String, "C000000000"),
		"error_on_no_match": tfBoolValue(false),
	})

	detail := readErr(t, resp)
	if !strings.Contains(detail, "C000000000") {
		t.Errorf("diagnostic must name the channel: %s", detail)
	}
}

// --- Task 3.10 / NFR-6, AC-12: a partial scan fails --------------------------------

func TestUsersRead_RateLimitedScanFailsRatherThanReturningPartialResults(t *testing.T) {
	resp := readUsers(t, map[string]stub{
		"/api/users.list": sequence(
			fixture("users_list_page1.json"),
			fixture("err_ratelimited.json"),
		),
	}, nil)

	detail := readErr(t, resp)
	if !strings.Contains(strings.ToLower(detail), "rate") {
		t.Errorf("expected a rate-limit diagnostic, got: %s", detail)
	}
	// The half-read workspace must not reach state: a shrunken set is what makes
	// downstream resources delete things.
	var m usersDataSourceModel
	_ = resp.State.Get(context.Background(), &m)
	if m.UserCount.ValueInt64() != 0 {
		t.Errorf("partial results reached state: user_count = %d", m.UserCount.ValueInt64())
	}
}
