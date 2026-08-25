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
