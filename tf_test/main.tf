terraform {
  required_providers {
    slack = {
      source = "pippiio.com/pippiio/slack"
    }
  }
}

provider "slack" {}

data "slack_hello_world" "this" {}

output "slack_hello_world" {
  value = data.slack_hello_world.this
}
