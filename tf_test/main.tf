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
  slack_ids = ["U0001", "U0002", "C0001"]
}
