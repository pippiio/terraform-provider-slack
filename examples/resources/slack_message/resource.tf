data "slack_user" "recipients" {
  for_each = toset(["u1", "u2"])
  name     = each.value
}

resource "slack_message" "this" {
  message   = "test"
  slack_ids = toset([for u in data.slack_user.recipients : u.id])
}
