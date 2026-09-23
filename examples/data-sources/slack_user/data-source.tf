# Look a user up by Slack user ID.
# Requires the `users:read` scope.
data "slack_user" "by_id" {
  id = "W012A3CDE"
}

# Look the same user up by email address.
# Requires the `users:read.email` scope, which is separate from `users:read`.
# Note that this lookup does not match deactivated accounts -- use `id` for those.
data "slack_user" "by_email" {
  email = "spengler@ghostbusters.example.com"
}

# Exactly one of `id` or `email` must be set. Setting both, or neither, is a
# configuration error caught before any API call is made.

output "display_name" {
  value = data.slack_user.by_id.profile.display_name
}

output "timezone" {
  value = data.slack_user.by_id.tz
}

# Attributes Slack does not return are null rather than empty strings. `profile.email`
# is null when the token lacks the `users:read.email` scope.
output "email" {
  value = data.slack_user.by_id.profile.email
}

# Send a message to every non-bot, active user in a list.
data "slack_user" "team" {
  for_each = toset(["W012A3CDE", "W07QCRPA4"])
  id       = each.value
}

resource "slack_message" "announcement" {
  message = "Deploy complete."
  slack_ids = toset([
    for u in data.slack_user.team : u.id
    if !u.is_bot && !u.deleted
  ])
}

# Custom profile fields are workspace-defined, so they are keyed by Slack's field ID
# rather than named in the schema. Map IDs to labels with Slack's team.profile.get.
output "team_custom_field" {
  value = try(data.slack_user.by_id.profile.fields["Xf0123456"].value, null)
}

# Null when Slack omitted the key; an empty map when the user simply has none set.
output "all_custom_fields" {
  value = {
    for id, field in coalesce(data.slack_user.by_id.profile.fields, {}) :
    id => field.value
  }
}

# Look a user up by their Slack handle (the `name` field), for configurations that only
# know usernames. This replaces the deprecated `slack_user_ids` data source.
#
# Slack has no lookup-by-username endpoint, so this scans `users.list` -- a Tier 2
# method limited to roughly 20 requests per minute. Prefer `id` or `email` where you
# have one, and keep `for_each` sets over usernames small.
data "slack_user" "by_name" {
  name = "glinda"
}

output "resolved_id" {
  value = data.slack_user.by_name.id
}

# Migrating from `slack_user_ids`:
#
#   data "slack_user_ids" "this" {
#     usernames = ["u1", "u2"]
#   }
#   # data.slack_user_ids.this.slack_ids["u1"]
#
# becomes:
data "slack_user" "by_username" {
  for_each = toset(["u1", "u2"])
  name     = each.value
}

output "slack_ids" {
  value = { for name, u in data.slack_user.by_username : name => u.id }
}
