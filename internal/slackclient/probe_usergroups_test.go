package slackclient

// Story: Phase 1 usergroups reconnaissance
//
// Answers the schema-shaping questions that cannot be settled from documentation:
//   Q1  Does usergroups.users.update accept an empty user list?   -> FR-8 validator or not
//   Q2  Is `handle` mutable via usergroups.update?                -> RequiresReplace or not
//   Q3  What error does create return against a disabled handle?  -> FR-3 adopt branch
//   Q4  Does usergroups.list return prefs.channels?               -> FR-9a plain Read or carry-forward
//   Q5  Is the workspace on a paid plan?                          -> whether anything can be verified live
//
// SAFETY: creates a real user group with a timestamped handle, then disables it. Slack has
// no delete, so the disabled group is left behind by design -- that is the very constraint
// this track exists to handle. Use a workspace where that is acceptable.
//
// Requires usergroups:read and usergroups:write on the token.
//
// Run with:
//   SLACK_PROBE_TOKEN=xoxb-... go test ./internal/slackclient/ -run TestProbe_UserGroups -v

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestProbe_UserGroups(t *testing.T) {
	token := os.Getenv("SLACK_PROBE_TOKEN")
	if token == "" {
		t.Skip("usergroups probe skipped: set SLACK_PROBE_TOKEN to run")
	}

	host := os.Getenv("SLACK_PROBE_HOST")
	if host == "" {
		host = "https://slack.com"
	}
	httpClient := &http.Client{Timeout: 15 * time.Second}

	// call issues a request the way requests.go does and returns status, body, ok, error code.
	call := func(method, endpoint string, params map[string]string) (int, string, bool, string) {
		t.Helper()
		req, err := http.NewRequest(method, fmt.Sprintf("%s/api/%s", host, endpoint), nil)
		if err != nil {
			t.Fatalf("building request for %s: %v", endpoint, err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		q := req.URL.Query()
		for k, v := range params {
			q.Add(k, v)
		}
		req.URL.RawQuery = q.Encode()

		res, err := httpClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, endpoint, err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)

		var env struct {
			Ok    bool   `json:"ok"`
			Error string `json:"error"`
		}
		_ = json.Unmarshal(body, &env)
		return res.StatusCode, string(body), env.Ok, env.Error
	}

	line := strings.Repeat("=", 71)

	// --- Q5: paid plan? -----------------------------------------------------
	_, body, ok, errCode := call(http.MethodGet, "usergroups.list", map[string]string{
		"include_disabled": "true",
	})
	t.Logf("Q5 usergroups.list -> ok=%v error=%q", ok, errCode)
	if !ok {
		t.Logf("body: %s", body)
		if errCode == "paid_only" {
			t.Log(line)
			t.Log("VERDICT Q5: workspace is NOT on a paid plan.")
			t.Log("=> User groups are unavailable here. AC-1e / AC-3a / AC-4 cannot be")
			t.Log("=> verified live in this track. Unit tests are unaffected.")
			t.Log(line)
			t.Fatal("cannot continue: usergroups unavailable on this plan")
		}
		if errCode == "missing_scope" {
			t.Fatal("token lacks usergroups:read -- add usergroups:read and usergroups:write, reinstall, retry")
		}
		t.Fatalf("unexpected failure listing usergroups: %s", errCode)
	}
	t.Log("VERDICT Q5: workspace IS on a paid plan; usergroups available.")

	// --- Q4: does list return prefs.channels? -------------------------------
	returnsPrefs := strings.Contains(body, `"prefs"`)
	returnsChannels := strings.Contains(body, `"channels"`)
	t.Logf("Q4 usergroups.list contains \"prefs\"=%v  \"channels\"=%v", returnsPrefs, returnsChannels)

	// --- setup: create a throwaway group ------------------------------------
	stamp := time.Now().UTC().Format("20060102150405")
	handle := "draft-probe-" + stamp
	_, body, ok, errCode = call(http.MethodPost, "usergroups.create", map[string]string{
		"name":        "draft probe " + stamp,
		"handle":      handle,
		"description": "temporary group created by the draft usergroups probe",
	})
	t.Logf("setup usergroups.create -> ok=%v error=%q", ok, errCode)
	if !ok {
		t.Fatalf("could not create probe group: %s", body)
	}
	var created struct {
		UserGroup struct {
			ID string `json:"id"`
		} `json:"usergroup"`
	}
	_ = json.Unmarshal([]byte(body), &created)
	groupID := created.UserGroup.ID
	t.Logf("created probe group %s (@%s)", groupID, handle)

	// --- Q1: empty user list accepted? --------------------------------------
	_, body, ok, errCode = call(http.MethodPost, "usergroups.users.update", map[string]string{
		"usergroup": groupID,
		"users":     "",
	})
	t.Logf("Q1 usergroups.users.update users=\"\" -> ok=%v error=%q", ok, errCode)
	t.Logf("Q1 raw body: %s", body)
	emptyAccepted := ok

	// --- Q2: handle mutable? ------------------------------------------------
	newHandle := handle + "-renamed"
	_, body, ok, errCode = call(http.MethodPost, "usergroups.update", map[string]string{
		"usergroup": groupID,
		"handle":    newHandle,
	})
	t.Logf("Q2 usergroups.update handle -> ok=%v error=%q", ok, errCode)
	handleMutable := ok
	if ok {
		handle = newHandle
	}

	// --- cleanup + Q3: create against a disabled handle ---------------------
	_, body, ok, errCode = call(http.MethodPost, "usergroups.disable", map[string]string{
		"usergroup": groupID,
	})
	t.Logf("cleanup usergroups.disable -> ok=%v error=%q", ok, errCode)
	if !ok {
		t.Logf("WARNING: probe group %s could not be disabled; clean it up manually. body=%s", groupID, body)
	}

	_, body, ok, errCode = call(http.MethodPost, "usergroups.create", map[string]string{
		"name":   "draft probe collision " + stamp,
		"handle": handle,
	})
	t.Logf("Q3 usergroups.create against DISABLED handle %q -> ok=%v error=%q", handle, ok, errCode)
	t.Logf("Q3 raw body: %s", body)
	collisionCode := errCode
	if ok {
		t.Log("Q3 NOTE: create SUCCEEDED against a disabled handle -- disable it too")
		var dup struct {
			UserGroup struct {
				ID string `json:"id"`
			} `json:"usergroup"`
		}
		_ = json.Unmarshal([]byte(body), &dup)
		_, _, _, _ = call(http.MethodPost, "usergroups.disable", map[string]string{"usergroup": dup.UserGroup.ID})
	}

	// --- verdict ------------------------------------------------------------
	t.Log(line)
	t.Logf("Q1 empty user list accepted        = %v", emptyAccepted)
	if emptyAccepted {
		t.Log("   => do NOT add the FR-8 non-empty validator (spec default holds)")
	} else {
		t.Log("   => Slack rejects empty lists: ADD the FR-8 validator (plan Task 3.13)")
	}
	t.Logf("Q2 handle mutable via update       = %v", handleMutable)
	if handleMutable {
		t.Log("   => handle needs NO RequiresReplace (spec default holds)")
	} else {
		t.Log("   => handle is immutable: add RequiresReplace, and document that a rename")
		t.Log("      disables the old group and leaves it as a reserved orphan")
	}
	t.Logf("Q3 create-on-disabled-handle error = %q", collisionCode)
	t.Log("   => confirms the FR-3 adopt branch is reachable (adopt checks list first,")
	t.Log("      so this is informational rather than load-bearing)")
	t.Logf("Q4 list returns prefs.channels     = %v", returnsChannels)
	if returnsChannels {
		t.Log("   => Read can populate channels directly (FR-9a carry-forward still harmless)")
	} else {
		t.Log("   => channels is WRITE-ONLY: FR-9a carry-forward is REQUIRED or every plan")
		t.Log("      shows a phantom diff")
	}
	t.Logf("probe group %s left DISABLED (Slack has no delete -- constraint C1)", groupID)
	t.Log("Record these in draft/tracks/slack-usergroup-resource/spec.md (Phase 1 findings).")
	t.Log(line)
}
