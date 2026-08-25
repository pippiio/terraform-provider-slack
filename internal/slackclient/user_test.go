package slackclient

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	return b
}

func decodeUser(t *testing.T, name string) *User {
	t.Helper()
	var res userResponse
	if err := json.Unmarshal(loadFixture(t, name), &res); err != nil {
		t.Fatalf("unmarshalling %s: %v", name, err)
	}
	return &res.User
}

// AC-2a: every field in the spec's schema must be populated from a full response.
func TestUser_UnmarshalsFullResponse(t *testing.T) {
	u := decodeUser(t, "users_info_full.json")

	if u.ID != "W012A3CDE" {
		t.Errorf("ID = %q, want W012A3CDE", u.ID)
	}
	if u.TeamID != "T012AB3C4" {
		t.Errorf("TeamID = %q, want T012AB3C4", u.TeamID)
	}
	if u.Name != "spengler" {
		t.Errorf("Name = %q, want spengler", u.Name)
	}
	if u.Deleted == nil || *u.Deleted {
		t.Errorf("Deleted = %v, want false", u.Deleted)
	}
	if u.RealName == nil || *u.RealName != "Egon Spengler" {
		t.Errorf("RealName = %v, want Egon Spengler", u.RealName)
	}
	if u.TZ == nil || *u.TZ != "America/Los_Angeles" {
		t.Errorf("TZ = %v, want America/Los_Angeles", u.TZ)
	}
	if u.TZOffset == nil || *u.TZOffset != -25200 {
		t.Errorf("TZOffset = %v, want -25200", u.TZOffset)
	}
	if u.IsAdmin == nil || !*u.IsAdmin {
		t.Errorf("IsAdmin = %v, want true", u.IsAdmin)
	}
	if u.IsBot == nil || *u.IsBot {
		t.Errorf("IsBot = %v, want false", u.IsBot)
	}
	if u.Updated == nil || *u.Updated != 1502138686 {
		t.Errorf("Updated = %v, want 1502138686", u.Updated)
	}
}

func TestUser_UnmarshalsFullProfile(t *testing.T) {
	p := decodeUser(t, "users_info_full.json").Profile

	if p.Email == nil || *p.Email != "spengler@ghostbusters.example.com" {
		t.Errorf("Email = %v, want the address", p.Email)
	}
	if p.DisplayName == nil || *p.DisplayName != "spengler" {
		t.Errorf("DisplayName = %v, want spengler", p.DisplayName)
	}
	if p.Title == nil || *p.Title != "Paranormal Investigator" {
		t.Errorf("Title = %v, want the title", p.Title)
	}
	if p.Phone == nil || *p.Phone != "555-0100" {
		t.Errorf("Phone = %v, want 555-0100", p.Phone)
	}
	if p.StatusText == nil || *p.StatusText != "Print is dead" {
		t.Errorf("StatusText = %v, want 'Print is dead'", p.StatusText)
	}
	if p.StatusExpiration == nil || *p.StatusExpiration != 1502138686 {
		t.Errorf("StatusExpiration = %v, want 1502138686", p.StatusExpiration)
	}
	if p.Image512 == nil || *p.Image512 != "https://a.slack-edge.com/512.png" {
		t.Errorf("Image512 = %v, want the 512 URL", p.Image512)
	}
	if p.IsCustomImage == nil || !*p.IsCustomImage {
		t.Errorf("IsCustomImage = %v, want true", p.IsCustomImage)
	}
	if p.FirstName == nil || *p.FirstName != "Egon" {
		t.Errorf("FirstName = %v, want Egon", p.FirstName)
	}
}

// AC-2c / FR-8: a field absent from the response must be distinguishable from an empty
// value, so the data source can render it null rather than "".
func TestUser_AbsentEmailIsNilNotEmptyString(t *testing.T) {
	p := decodeUser(t, "users_info_no_email_scope.json").Profile

	if p.Email != nil {
		t.Errorf("Email = %q, want nil when the response omits it (no users:read.email)", *p.Email)
	}
	if p.Title != nil {
		t.Errorf("Title = %q, want nil when absent", *p.Title)
	}
	// Fields that ARE present must still decode.
	if p.DisplayName == nil || *p.DisplayName != "spengler" {
		t.Errorf("DisplayName = %v, want spengler", p.DisplayName)
	}
}

func TestUser_BotAccountFields(t *testing.T) {
	u := decodeUser(t, "users_info_bot.json")

	if u.IsBot == nil || !*u.IsBot {
		t.Errorf("IsBot = %v, want true", u.IsBot)
	}
	if u.Profile.BotID == nil || *u.Profile.BotID != "B023BCDE1" {
		t.Errorf("Profile.BotID = %v, want B023BCDE1", u.Profile.BotID)
	}
	if u.Profile.APIAppID == nil || *u.Profile.APIAppID != "A012B3CDE" {
		t.Errorf("Profile.APIAppID = %v, want A012B3CDE", u.Profile.APIAppID)
	}
	if u.Profile.Email != nil {
		t.Errorf("bot Email = %v, want nil", u.Profile.Email)
	}
}

// --- GetUserByID / GetUserByEmail ---

func TestGetUserByID_Success(t *testing.T) {
	c, rec := newTestClient(t, routes{
		"/api/users.info": fixture("users_info_full.json"),
	})

	u, err := c.GetUserByID("W012A3CDE")
	if err != nil {
		t.Fatalf("GetUserByID returned error: %v", err)
	}
	if u.ID != "W012A3CDE" {
		t.Errorf("ID = %q, want W012A3CDE", u.ID)
	}
	if u.Profile.Email == nil || *u.Profile.Email != "spengler@ghostbusters.example.com" {
		t.Errorf("Profile.Email = %v, want the address", u.Profile.Email)
	}

	req := rec.last()
	if req.Path != "/api/users.info" {
		t.Errorf("path = %q, want /api/users.info", req.Path)
	}
	if got := req.Query.Get("user"); got != "W012A3CDE" {
		t.Errorf("query user = %q, want W012A3CDE", got)
	}
	if req.Authorization != "Bearer xoxb-test-token" {
		t.Errorf("Authorization = %q, want the bearer token", req.Authorization)
	}
}

// AC-1e: an unknown user must produce an error, not a zero-valued success.
func TestGetUserByID_NotFoundIsAnError(t *testing.T) {
	c, _ := newTestClient(t, routes{
		"/api/users.info": fixture("err_users_not_found.json"),
	})

	u, err := c.GetUserByID("U000000000")
	if err == nil {
		t.Fatalf("expected an error for an unknown user, got user=%+v", u)
	}
	if u != nil {
		t.Errorf("user = %+v, want nil on error", u)
	}
	if got := ErrorCode(err); got != "users_not_found" {
		t.Errorf("ErrorCode = %q, want users_not_found", got)
	}
}

func TestGetUserByEmail_Success(t *testing.T) {
	c, rec := newTestClient(t, routes{
		"/api/users.lookupByEmail": fixture("users_info_full.json"),
	})

	u, err := c.GetUserByEmail("spengler@ghostbusters.example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail returned error: %v", err)
	}
	if u.ID != "W012A3CDE" {
		t.Errorf("ID = %q, want W012A3CDE", u.ID)
	}

	req := rec.last()
	if req.Path != "/api/users.lookupByEmail" {
		t.Errorf("path = %q, want /api/users.lookupByEmail", req.Path)
	}
	if got := req.Query.Get("email"); got != "spengler@ghostbusters.example.com" {
		t.Errorf("query email = %q, want the address", got)
	}
}

// AC-4: without users:read.email the lookup fails with missing_scope, and the code must
// reach the caller so it can name the scope in a diagnostic.
func TestGetUserByEmail_MissingScopeSurfacesCode(t *testing.T) {
	c, _ := newTestClient(t, routes{
		"/api/users.lookupByEmail": fixture("err_missing_scope.json"),
	})

	_, err := c.GetUserByEmail("spengler@ghostbusters.example.com")
	if err == nil {
		t.Fatal("expected an error when the scope is missing")
	}
	if got := ErrorCode(err); got != "missing_scope" {
		t.Errorf("ErrorCode = %q, want missing_scope", got)
	}
}

// AC-1f: both endpoints decode into the same shape, so a user resolved either way
// yields identical attributes.
func TestGetUser_BothLookupsYieldIdenticalUser(t *testing.T) {
	byID, _ := newTestClient(t, routes{"/api/users.info": fixture("users_info_full.json")})
	byEmail, _ := newTestClient(t, routes{"/api/users.lookupByEmail": fixture("users_info_full.json")})

	a, err := byID.GetUserByID("W012A3CDE")
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	b, err := byEmail.GetUserByEmail("spengler@ghostbusters.example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}

	if a.ID != b.ID || a.Name != b.Name || a.TeamID != b.TeamID {
		t.Errorf("identity differs: byID=%+v byEmail=%+v", a, b)
	}
	if (a.Profile.Email == nil) != (b.Profile.Email == nil) {
		t.Fatal("email presence differs between lookup paths")
	}
	if a.Profile.Email != nil && *a.Profile.Email != *b.Profile.Email {
		t.Errorf("email differs: %q vs %q", *a.Profile.Email, *b.Profile.Email)
	}
}

// Defensive boundary: Slack answering ok:true with no user object must not yield a
// silently empty user written into Terraform state.
func TestGetUserByID_EmptyUserObjectIsAnError(t *testing.T) {
	c, _ := newTestClient(t, routes{
		"/api/users.info": raw(200, `{"ok":true}`),
	})

	u, err := c.GetUserByID("W012A3CDE")
	if err == nil {
		t.Fatalf("expected an error for a response with no user object, got %+v", u)
	}
	if u != nil {
		t.Errorf("user = %+v, want nil", u)
	}
}

func TestGetUserByEmail_EmptyUserObjectIsAnError(t *testing.T) {
	c, _ := newTestClient(t, routes{
		"/api/users.lookupByEmail": raw(200, `{"ok":true,"user":{}}`),
	})

	if _, err := c.GetUserByEmail("a@b.com"); err == nil {
		t.Fatal("expected an error for an empty user object")
	}
}
