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
