# Deprecated: this data source will be removed in v2.0.0.
# Use `slack_user` with the `name` argument instead -- it returns the full user object
# rather than the ID alone. See examples/data-sources/slack_user.
data "slack_user_ids" "this" {
  usernames = ["u1", "u2"]
}
