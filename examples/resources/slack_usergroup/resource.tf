# Slack user groups require a PAID Slack plan.
# On the free plan every usergroups API call fails with `paid_only`, and this resource
# cannot be used at all. Requires the `usergroups:read` and `usergroups:write` scopes.
#
# Note also that Slack workspaces can restrict who may create user groups. A correctly
# scoped token may still be refused with `permission_denied`.

# Authoritative membership: Terraform owns who is in the group.
resource "slack_usergroup" "engineering" {
  name        = "Engineering"
  handle      = "engineering" # the @mention abbreviation
  description = "Everyone who ships product code"

  # Default channels new members are added to.
  channels = ["C0611AAAA"]

  # WARNING: `users` is authoritative. Slack offers only a replace operation for
  # membership, so anyone added to this group by hand in Slack is removed on the next
  # apply -- and Slack sends no notification when that happens.
  users = [
    data.slack_user.alice.id,
    data.slack_user.bob.id,
  ]
}

data "slack_user" "alice" {
  email = "alice@example.com"
}

data "slack_user" "bob" {
  email = "bob@example.com"
}

# Slack-owned membership: omit `users` entirely and the provider never touches it.
# Use this when the group is populated by people, another tool, or an identity provider.
resource "slack_usergroup" "on_call" {
  name        = "On Call"
  handle      = "on-call"
  description = "Rotation membership is managed outside Terraform"
  # no `users` attribute -- membership is left to Slack
}

# IMPORTANT: `terraform destroy` DISABLES a user group rather than deleting it. Slack
# provides no delete, and a disabled group keeps its name and handle reserved. Creating a
# group with the same handle later re-enables and adopts the disabled one, which the
# provider reports as a warning.

output "engineering_id" {
  value = slack_usergroup.engineering.id
}

output "engineering_disabled" {
  value = slack_usergroup.engineering.is_disabled
}
