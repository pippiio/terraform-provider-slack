package slackclient

// Story: conversations.members
//
// Input:  a channel ID.
// Process:
//   1. Page through conversations.members with an explicit limit and cursor.
//   2. Accumulate the member IDs in the order Slack returned them.
//   3. Stop on an empty cursor, a cursor Slack repeats, or the page bound.
// Output: every member ID in the channel, or a *SlackError.
//
// Dependencies: pageThrough, which owns the cursor loop.
// Side effects: one HTTPS GET per page. No writes.
//
// Slack returns IDs here, never user objects, which is why the provider layer joins
// these against a users.list scan rather than calling users.info per member.
//
// An empty channel returns an empty slice and no error. Emptiness is a decision for the
// data source (FR-7), not for the client; a *missing* channel is an error here.
//
// Scopes: channels:read for public channels, groups:read for private ones, plus im:read
// and mpim:read for DMs. Slack answers channel_not_found for all of "no such channel",
// "you lack the scope", and "the token is not a member" -- indistinguishable here, which
// is why the diagnostic that names all three lives in the provider layer.

import (
	"encoding/json"
)

// channelMembersResponse is the envelope returned by conversations.members.
type channelMembersResponse struct {
	Members          []string `json:"members"`
	ResponseMetadata struct {
		NextCursor string `json:"next_cursor"`
	} `json:"response_metadata"`
}

// ListChannelMembers returns the Slack user IDs of every member of a channel.
//
// Requires the scope matching the channel's type; see the file comment.
func (c *Client) ListChannelMembers(channelID string) ([]string, error) {
	var members []string

	// Slack repeating a cursor means it is not advancing, and pageThrough stops -- but
	// only after the duplicate page has already been handed over. Deduplicating here
	// keeps that from surfacing as a member counted twice. A channel's membership is a
	// set, so this costs nothing in the normal case.
	seen := make(map[string]struct{})

	err := c.pageThrough("conversations.members", map[string]string{"channel": channelID},
		func(body []byte) (string, bool, error) {
			res := channelMembersResponse{}
			if err := json.Unmarshal(body, &res); err != nil {
				return "", false, err
			}
			for _, id := range res.Members {
				if _, dup := seen[id]; dup {
					continue
				}
				seen[id] = struct{}{}
				members = append(members, id)
			}
			return res.ResponseMetadata.NextCursor, false, nil
		})
	if err != nil {
		return nil, err
	}

	return members, nil
}
