
terraform {
  required_providers {
    slack = {
      source = "pippiio.com/pippiio/slack"
    }
  }
}

provider "slack" {
  host = "https://slack.com"

  # Bot token (xoxb-). Used by everything except user group management.
  # May also be supplied via SLACK_TOKEN.
  token = var.slack_bot_token

  # User token (xoxp-). OPTIONAL, and only needed to *manage* user groups with the
  # slack_usergroup resource: Slack refuses usergroups.create for bot tokens in
  # workspaces that restrict who may manage user groups, answering permission_denied.
  #
  # Reading user groups works with the bot token, so the slack_usergroup data source
  # does not need this. May also be supplied via SLACK_USER_TOKEN.
  user_token = var.slack_user_token
}

variable "slack_bot_token" {
  type      = string
  sensitive = true
}

variable "slack_user_token" {
  type      = string
  sensitive = true
  default   = null
}
