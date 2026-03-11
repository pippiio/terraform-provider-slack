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

resource "slack_message" "this" {
  message   = "test"
  slack_ids = ["user1", "user2", "channel"]
}
