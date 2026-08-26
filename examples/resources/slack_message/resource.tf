
resource "slack_message" "this" {
  message   = "test"
  slack_ids = toset(values(data.slack_user_ids.this.slack_ids))
}

# The message text is visible in plan output, so a change to it reviews as a normal
# diff. Wrap the value with sensitive() when a particular message should not be
# printed -- Terraform then renders it as "(sensitive value)".
#
# This hides the text from output only. Terraform state stores it in plain text
# either way, so state must still be treated as secret.
resource "slack_message" "announcement" {
  message   = sensitive(var.release_note)
  slack_ids = toset(values(data.slack_user_ids.this.slack_ids))
}

variable "release_note" {
  type = string
}
