
terraform {
  required_providers {
    slack = {
      source = "pippiio.com/pippiio/slack"
    }
  }
}

# host/token/user_token come from SLACK_HOST, SLACK_TOKEN and SLACK_USER_TOKEN so this
# file carries no credentials.
provider "slack" {}

# ---------------------------------------------------------------------------
# Existing surface
# ---------------------------------------------------------------------------

data "slack_user_ids" "this" {
  usernames = ["u1", "u2"]
}

resource "slack_message" "this" {
  message   = "test"
  slack_ids = toset(values(data.slack_user_ids.this.slack_ids))
}

data "slack_user" "by_id" {
  id = "W012A3CDE"
}

data "slack_user" "by_email" {
  email = "spengler@ghostbusters.example.com"
}

# ---------------------------------------------------------------------------
# User groups
#
# Requires a PAID Slack plan, the usergroups:read + usergroups:write scopes, and a USER
# token (SLACK_USER_TOKEN) for the resource. The data source needs only the bot token.
#
# NOTE: `terraform destroy` DISABLES a user group -- Slack has no delete -- and its handle
# stays reserved afterwards. Re-applying adopts the disabled group and emits a warning.
# ---------------------------------------------------------------------------

# Read an existing group. Bot token is sufficient here.
data "slack_usergroup" "existing" {
  handle = "example-alpha"
}

# Slack-owned membership: `users` omitted, so the provider never touches it.
resource "slack_usergroup" "smoke_no_members" {
  name        = "draft smoke test"
  handle      = "draft-smoke-test"
  description = "Created by tf_test for manual verification. Safe to disable."
}

# Terraform-owned membership. `users` must be non-empty when set -- omit it instead to
# leave membership to Slack.
resource "slack_usergroup" "smoke_with_members" {
  name        = "draft smoke test members"
  handle      = "draft-smoke-test-members"
  description = "Membership is authoritative: manual additions are removed on apply."

  users = [data.slack_user.by_id.id]
}

output "existing_group_members" {
  value = data.slack_usergroup.existing.users
}

output "smoke_group_id" {
  value = slack_usergroup.smoke_no_members.id
}

output "smoke_group_disabled" {
  value = slack_usergroup.smoke_no_members.is_disabled
}

# Groups synced from an identity provider own their own membership -- setting `users` on
# one is refused with a diagnostic.
output "existing_membership_externally_owned" {
  value = (
    data.slack_usergroup.existing.is_idp_group ||
    data.slack_usergroup.existing.is_membership_locked
  )
}
