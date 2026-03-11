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
  type      = ""
  message   = "test"
  slack_ids = "user"
}
