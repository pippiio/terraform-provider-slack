package provider

// Story: slack_users selection, filtering and result mapping
//
// Input:  the parsed config, plus each page of users the scan yields.
// Process:
//   1. Build a selector from whichever of emails / usernames / channel was set.
//   2. Per user, ask the selector whether it matches, and which input it satisfied.
//   3. Once every input has matched, tell the scan it can stop.
//   4. Apply the tri-state filters to what matched.
//   5. Turn the survivors into the map, ID set and count the schema declares.
// Output: the computed half of usersDataSourceModel, or diagnostics.
//
// Dependencies: userToObject / userAttrTypes from slack_user_mapping.go.
// Side effects: none -- the API calls happen in Read; this is all pure.
//
// Matching runs before filtering, deliberately (FR-5). An email naming a real account
// that a filter then removes is an empty result, not an unresolved input: reporting it
// as unresolved would send an operator hunting for an account that exists.

import (
	"context"
	"sort"
	"strings"

	"terraform-provider-slack/internal/slackclient"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// userSelector decides which scanned users the config asked for.
//
// The zero value matches everything, which is the no-selector case.
type userSelector struct {
	// wantEmails maps a normalised address to the input as the operator wrote it, so a
	// diagnostic can name what they typed rather than what we lowercased.
	wantEmails map[string]string
	// wantNames holds Slack handles, matched exactly. Slack handles are case-sensitive;
	// email addresses are not, which is why these are two maps and not one.
	wantNames map[string]struct{}
	// containerIDs holds the member IDs of a channel. Nil means no container.
	containerIDs map[string]struct{}
	// hasContainer separates "a container with no members" from "no container".
	hasContainer bool
}

// normaliseEmail lowercases an address for comparison. Slack treats the local part as
// case-insensitive in lookupByEmail, and an operator who types Firstname.Lastname@ in a
// tfvars file matching nothing would read as a bug rather than as a rule.
func normaliseEmail(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// match reports whether a user is selected, and which input satisfied it. The input is
// empty for selectors that are not input lists.
func (s userSelector) match(u *slackclient.User) (input string, ok bool) {
	switch {
	case len(s.wantEmails) > 0:
		if u.Profile.Email == nil {
			return "", false
		}
		if written, found := s.wantEmails[normaliseEmail(*u.Profile.Email)]; found {
			return written, true
		}
		return "", false

	case len(s.wantNames) > 0:
		if _, found := s.wantNames[u.Name]; found {
			return u.Name, true
		}
		return "", false

	case s.hasContainer:
		_, found := s.containerIDs[u.ID]
		return "", found

	default:
		return "", true
	}
}

// inputCount is how many distinct inputs the selector is waiting to satisfy. Zero means
// the selector is not input-driven and the scan must read every page.
func (s userSelector) inputCount() int {
	if n := len(s.wantEmails); n > 0 {
		return n
	}
	return len(s.wantNames)
}

// unresolved lists the inputs nothing matched, as written, sorted for a stable message.
func (s userSelector) unresolved(matched map[string]struct{}) []string {
	var out []string
	for _, written := range s.wantEmails {
		if _, ok := matched[written]; !ok {
			out = append(out, written)
		}
	}
	for name := range s.wantNames {
		if _, ok := matched[name]; !ok {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// userFilters is the tri-state filter set. A nil field means the filter was not
// configured and is not applied.
type userFilters struct {
	isBot             *bool
	deleted           *bool
	isRestricted      *bool
	isUltraRestricted *bool
	isAdmin           *bool
	isAppUser         *bool
}

// keep reports whether a user survives every configured filter.
//
// An absent flag counts as false. Slack omits most of these booleans rather than
// sending false, so `is_bot = false` has to mean "not a bot" including the users Slack
// said nothing about -- otherwise the commonest filter in the data source would exclude
// most of the humans it exists to find. This is the one place the provider collapses
// null into a value, and it is a filtering decision only: the attribute the data source
// *returns* stays null, preserving what Slack actually said.
func (f userFilters) keep(u *slackclient.User) bool {
	checks := []struct {
		want *bool
		got  *bool
	}{
		{f.isBot, u.IsBot},
		{f.deleted, u.Deleted},
		{f.isRestricted, u.IsRestricted},
		{f.isUltraRestricted, u.IsUltraRestricted},
		{f.isAdmin, u.IsAdmin},
		{f.isAppUser, u.IsAppUser},
	}

	for _, c := range checks {
		if c.want == nil {
			continue
		}
		actual := c.got != nil && *c.got
		if actual != *c.want {
			return false
		}
	}
	return true
}

// usersToState turns the surviving users into the computed attributes.
func usersToState(ctx context.Context, users []*slackclient.User) (types.Map, types.Set, types.Int64, diag.Diagnostics) {
	var diags diag.Diagnostics
	elemType := types.ObjectType{AttrTypes: userAttrTypes()}

	objects := make(map[string]attr.Value, len(users))
	ids := make([]attr.Value, 0, len(users))

	for _, u := range users {
		obj, d := userToObject(ctx, u)
		diags.Append(d...)
		if d.HasError() {
			continue
		}
		objects[u.ID] = obj
		ids = append(ids, types.StringValue(u.ID))
	}

	userMap, d := types.MapValue(elemType, objects)
	diags.Append(d...)

	idSet, d := types.SetValue(types.StringType, ids)
	diags.Append(d...)

	return userMap, idSet, types.Int64Value(int64(len(objects))), diags
}
