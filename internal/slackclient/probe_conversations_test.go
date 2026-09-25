package slackclient

// Story: Task 1.2 -- the conversations.members probe
//
// Input:  a real Slack token and a channel ID, supplied via environment.
// Process:
//   1. Call conversations.members with an explicit small limit, so a channel of any
//      size produces a second page.
//   2. Print the raw body: the members array shape, and whether
//      response_metadata.next_cursor is present and non-empty.
//   3. If a cursor came back, call again with it and confirm the page advances.
//   4. Cross-reference the returned IDs against users.info, and report how many are
//      bots and how many are deleted -- spec Q2.
//   5. Dump the response headers once, so we can see what Slack actually returns.
// Output: a verdict printed to the test log, to be recorded in spec.md.
//
// Unlike the A-2 probe, this one is READ-ONLY. It creates nothing and deletes nothing,
// so it is safe against a real channel.
//
// On step 5 and the Tier 4 claim: Slack does not advertise a method's rate-limit tier
// in any response header, so this probe cannot confirm it without deliberately getting
// rate-limited, which is not worth doing. The header dump records what is actually
// there -- notably Retry-After, which only appears on a 429. Treat the tier as
// documented-not-observed and say so in the spec rather than implying it was measured.
//
// SAFETY: needs a channel the token can see. A private channel additionally needs
// groups:read, which is itself worth observing -- FR-10 claims channel_not_found covers
// that case, and this is the cheapest way to confirm it.
//
// Run with:
//   SLACK_PROBE_TOKEN=xoxb-... SLACK_PROBE_CHANNEL=C0123456789 \
//     go test ./internal/slackclient/ -run TestProbe_ConversationsMembers -v

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
)

// probeMembersPageLimit is deliberately tiny. A real channel has more members than
// this, which is what forces Slack to return a cursor and lets step 3 observe paging.
const probeMembersPageLimit = 2

// probeClassifyCap bounds step 4. Classifying every member of a large channel would be
// one users.info call each; the question is only whether bots and deactivated accounts
// appear at all, which a sample answers.
const probeClassifyCap = 25

func TestProbe_ConversationsMembers(t *testing.T) {
	token := os.Getenv("SLACK_PROBE_TOKEN")
	channel := os.Getenv("SLACK_PROBE_CHANNEL")
	if token == "" || channel == "" {
		t.Skip("conversations.members probe skipped: set SLACK_PROBE_TOKEN and SLACK_PROBE_CHANNEL to run")
	}

	host := os.Getenv("SLACK_PROBE_HOST")
	if host == "" {
		host = "https://slack.com"
	}

	httpClient := &http.Client{Timeout: 10 * time.Second}

	// call issues a request the same way requests.go does -- query-string args, nil
	// body, bearer header -- and returns everything the probe might want to look at.
	call := func(endpoint string, params map[string]string) (int, http.Header, string) {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/%s", host, endpoint), nil)
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
			t.Fatalf("GET %s: %v", endpoint, err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		return res.StatusCode, res.Header, string(body)
	}

	type membersResponse struct {
		Ok               bool     `json:"ok"`
		Error            string   `json:"error"`
		Members          []string `json:"members"`
		ResponseMetadata struct {
			NextCursor string `json:"next_cursor"`
		} `json:"response_metadata"`
	}

	// --- Step 1: first page ------------------------------------------------------
	status, headers, body := call("conversations.members", map[string]string{
		"channel": channel,
		"limit":   fmt.Sprint(probeMembersPageLimit),
	})

	var page1 membersResponse
	if err := json.Unmarshal([]byte(body), &page1); err != nil {
		t.Fatalf("decoding first page: %v -- body=%s", err, body)
	}

	t.Logf("conversations.members page 1 -> status=%d ok=%v error=%q members=%d",
		status, page1.Ok, page1.Error, len(page1.Members))
	t.Logf("conversations.members page 1 raw body: %s", body)

	if !page1.Ok {
		t.Logf("=> the call FAILED with %q.", page1.Error)
		if page1.Error == "channel_not_found" {
			t.Log("=> FR-10 evidence: note which of the three causes applied here --")
			t.Log("=>   wrong ID / private channel without groups:read / token not a member.")
			t.Log("=> Slack gives the same code for all three, which is the point of FR-10.")
		}
		t.Fatalf("cannot continue the probe without a readable channel")
	}

	// --- Step 2 & 3: pagination ---------------------------------------------------
	cursor := page1.ResponseMetadata.NextCursor
	t.Logf("next_cursor present=%v value=%q", cursor != "", cursor)

	paginates := false
	allMembers := append([]string(nil), page1.Members...)

	if cursor == "" {
		t.Logf("WARNING: no cursor returned for a limit of %d. Either the channel has "+
			"%d or fewer members -- use a bigger one -- or conversations.members does "+
			"not honour limit the way users.list does.", probeMembersPageLimit, probeMembersPageLimit)
	} else {
		_, _, body2 := call("conversations.members", map[string]string{
			"channel": channel,
			"limit":   fmt.Sprint(probeMembersPageLimit),
			"cursor":  cursor,
		})

		var page2 membersResponse
		if err := json.Unmarshal([]byte(body2), &page2); err != nil {
			t.Fatalf("decoding second page: %v -- body=%s", err, body2)
		}
		t.Logf("conversations.members page 2 -> ok=%v error=%q members=%d next_cursor=%q",
			page2.Ok, page2.Error, len(page2.Members), page2.ResponseMetadata.NextCursor)

		// The page must actually advance. A cursor that returns the same members is
		// exactly the non-advancing case ListChannelMembers has to be bounded against.
		overlap := 0
		first := map[string]bool{}
		for _, id := range page1.Members {
			first[id] = true
		}
		for _, id := range page2.Members {
			if first[id] {
				overlap++
			}
		}
		paginates = page2.Ok && len(page2.Members) > 0 && overlap == 0
		t.Logf("page 2 overlap with page 1: %d member(s) -- advances cleanly = %v", overlap, paginates)

		allMembers = append(allMembers, page2.Members...)
	}

	// --- Step 4: Q2 -- do bots and deactivated members appear? ---------------------
	if len(allMembers) > probeClassifyCap {
		allMembers = allMembers[:probeClassifyCap]
	}

	bots, deleted, classified := 0, 0, 0
	for _, id := range allMembers {
		_, _, ub := call("users.info", map[string]string{"user": id})
		var ur struct {
			Ok   bool `json:"ok"`
			User struct {
				Name    string `json:"name"`
				IsBot   bool   `json:"is_bot"`
				Deleted bool   `json:"deleted"`
			} `json:"user"`
		}
		if err := json.Unmarshal([]byte(ub), &ur); err != nil || !ur.Ok {
			t.Logf("could not classify %s: %s", id, ub)
			continue
		}
		classified++
		if ur.User.IsBot {
			bots++
		}
		if ur.User.Deleted {
			deleted++
		}
		t.Logf("  member %s name=%-20s is_bot=%-5v deleted=%v", id, ur.User.Name, ur.User.IsBot, ur.User.Deleted)
	}

	// --- Step 5: what does Slack actually send back? -------------------------------
	names := make([]string, 0, len(headers))
	for k := range headers {
		names = append(names, k)
	}
	sort.Strings(names)
	t.Log("response headers on conversations.members:")
	for _, k := range names {
		t.Logf("  %s: %s", k, strings.Join(headers[k], ", "))
	}

	t.Log("=======================================================================")
	t.Logf("PROBE VERDICT: conversations.members paginates by cursor = %v", paginates)
	t.Logf("PROBE VERDICT: of %d classified members, %d bots and %d deactivated", classified, bots, deleted)
	if bots > 0 || deleted > 0 {
		t.Log("=> Q2 ANSWERED YES: bots and/or deactivated members do appear.")
		t.Log("=> The tri-state filters are worth having on the channel selector.")
	} else {
		t.Log("=> Q2 INCONCLUSIVE from this channel: no bots or deactivated members in")
		t.Log("=> the sample. Re-run against a channel known to contain a bot before")
		t.Log("=> concluding they are excluded -- absence here is not evidence.")
	}
	t.Log("=> RATE LIMIT TIER: not observable. Slack does not put a method's tier in")
	t.Log("=> any header. Record it in the spec as documented-not-observed.")
	t.Log("Record this verdict in draft/tracks/slack-users-data-source/spec.md (FR-13).")
	t.Log("=======================================================================")
}
