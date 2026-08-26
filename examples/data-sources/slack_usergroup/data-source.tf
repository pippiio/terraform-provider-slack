# Slack user groups require a PAID Slack plan; on the free plan every usergroups call
# fails with `paid_only`. Requires the `usergroups:read` scope.

# Look a group up by its @mention handle.
data "slack_usergroup" "engineering" {
  handle = "engineering"
}

# Or by ID.
data "slack_usergroup" "by_id" {
  id = "S0615G0KT"
}

# Exactly one of `id` or `handle` must be set; setting both, or neither, is a
# configuration error caught before any API call is made.

output "member_ids" {
  value = data.slack_usergroup.engineering.users
}

# Disabled groups are returned too -- Slack has no delete, so a destroyed group is
# merely disabled and keeps its handle reserved.
output "is_disabled" {
  value = data.slack_usergroup.engineering.is_disabled
}

# Groups synced from an identity provider own their own membership. Check before
# attempting to manage members with a slack_usergroup resource.
output "membership_is_externally_owned" {
  value = (
    data.slack_usergroup.engineering.is_idp_group ||
    data.slack_usergroup.engineering.is_membership_locked
  )
}

# Send a message to every member of a group.
resource "slack_message" "standup" {
  message   = "Standup in 5 minutes"
  slack_ids = toset(data.slack_usergroup.engineering.users)
}
