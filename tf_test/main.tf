
terraform {
  required_providers {
    slack = {
      source = "pippiio.com/pippiio/slack"
    }
  }
}

provider "slack" {
  host  = "https://pippiio.com"
  token = "xoxb-1234567890"
}

# Deprecated, kept here so the deprecation warning is visible in a plan.
data "slack_user_ids" "this" {
  usernames = ["u1", "u2"]
}

resource "slack_message" "this" {
  message   = "test"
  slack_ids = toset(values(data.slack_user_ids.this.slack_ids))
}

# slack_user: single-user lookup by ID, email, or username.
data "slack_user" "by_id" {
  id = "W012A3CDE"
}

data "slack_user" "by_email" {
  email = "spengler@ghostbusters.example.com"
}

output "user_by_id_display_name" {
  value = data.slack_user.by_id.profile.display_name
}

output "user_by_email_id" {
  value = data.slack_user.by_email.id
}

# Null rather than "" when the token lacks users:read.email.
output "user_by_id_email" {
  value = data.slack_user.by_id.profile.email
}

# Lookup by Slack handle -- the replacement for slack_user_ids.
data "slack_user" "by_name" {
  name = "u1"
}

output "user_by_name_id" {
  value = data.slack_user.by_name.id
}

# ---------------------------------------------------------------------------
# slack_users: plural query with selectors and filters.
#
# Each read costs one paginated users.list pass, so keep an eye on how many of these
# a single plan resolves. Needs users:read; users:read.email for the emails selector;
# channels:read (or groups:read plus membership) for the channel selector.
# ---------------------------------------------------------------------------

# No selector: the whole workspace, filtered down to active humans.
data "slack_users" "humans" {
  is_bot  = false
  deleted = false
}

# Bulk username resolution -- the replacement for slack_user_ids.
data "slack_users" "by_username" {
  usernames = ["u1", "u2"]
}

# Channel membership. Replace with a real channel ID before running.
data "slack_users" "channel_members" {
  channel           = "C012AB3CD"
  is_bot            = false
  error_on_no_match = false
}

output "human_count" {
  value = data.slack_users.humans.user_count
}

output "usernames_to_ids" {
  value = { for id, u in data.slack_users.by_username.users : u.name => id }
}

# The full object is available, not just the ID.
output "human_timezones" {
  value = { for id, u in data.slack_users.humans.users : u.name => u.tz }
}
