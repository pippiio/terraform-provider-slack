
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

data "slack_user_ids" "this" {
  usernames = ["u1", "u2"]
}

resource "slack_message" "this" {
  message   = "test"
  slack_ids = toset(values(data.slack_user_ids.this.slack_ids))
}

# slack_user: single-user lookup by ID or email.
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
