terraform {
  required_providers {
    slack = {
      source = "pippiio.com/pippiio/slack"
    }
  }
}

provider "slack" {
  host  = "TEST"
  token = "TEST"
}

resource "slack_message" "this" {
}
