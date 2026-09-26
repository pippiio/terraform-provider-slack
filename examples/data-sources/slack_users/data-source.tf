# Every active human in the workspace.
#
# With no selector, slack_users returns everyone, and the filters do the work. This is
# the cheapest useful query: exactly one paginated pass over users.list.
data "slack_users" "humans" {
  is_bot  = false
  deleted = false
}

# Resolve a list of usernames in one pass. This is the replacement for the deprecated
# slack_user_ids data source -- and unlike `for_each` over slack_user, which costs one
# workspace scan per username, this costs one scan in total.
data "slack_users" "oncall" {
  usernames = ["u1", "u2"]
}

# Resolve email addresses. Matching is case-insensitive, unlike usernames, which are
# Slack handles and exact. Needs the `users:read.email` scope: without it Slack omits
# the field entirely and nothing can match.
data "slack_users" "by_email" {
  emails = ["egon@ghostbusters.example.com"]
}

# Everyone in a channel, minus the bots.
#
# `channel` takes an ID, not a name -- Slack's endpoint has no name form. Needs
# `channels:read` for a public channel, or `groups:read` plus membership for a private
# one. Filters apply to the members, so this is "the humans in #incidents".
data "slack_users" "incident_responders" {
  channel = "C012AB3CD"
  is_bot  = false
}

# The commonest use: message a set of people without hand-maintaining the list.
resource "slack_message" "standup" {
  message   = "Standup in five minutes."
  slack_ids = data.slack_users.incident_responders.user_ids
}

# users is keyed by Slack user ID, the only key available for every selector -- channel
# members arrive with no input to pair them with. Re-key with a `for` expression.
output "by_handle" {
  value = { for id, u in data.slack_users.humans.users : u.name => u.id }
}

# Each value is the full object slack_user exposes, so filtering and inspection can go
# well beyond the built-in attributes.
output "admins_in_europe" {
  value = [
    for id, u in data.slack_users.humans.users : u.name
    if u.is_admin && startswith(coalesce(u.tz, ""), "Europe/")
  ]
}

# An unresolved username or email fails the plan by default, naming each one that
# missed. That is deliberate: silently dropping a recipient is how a stale username
# removes someone from a set another resource treats as authoritative.
#
# Set error_on_no_match = false where an empty or partial result is legitimate.
data "slack_users" "best_effort" {
  usernames         = ["u1", "someone-who-may-have-left"]
  error_on_no_match = false
}

output "resolved_count" {
  value = data.slack_users.best_effort.user_count
}
