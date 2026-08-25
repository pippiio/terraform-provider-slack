# Changelog

All notable changes to this provider are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this
project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### ⚠️ Behavioural change — read before upgrading

**Slack API failures now fail your `terraform apply`.**

Previous versions checked only the HTTP status code. The Slack Web API reports
application-level failures — an invalid token, a missing OAuth scope, a channel the bot
is not a member of, rate limiting — as **HTTP 200** with `{"ok": false, "error": ...}` in
the response body. Those failures were therefore treated as successes: the apply reported
success while nothing had happened in Slack, and state was written with empty values.

Those same conditions now produce a Terraform error naming the underlying Slack error.

**What this means for you:** if a configuration has been failing silently, the next apply
after upgrading will surface it as a hard error. This is the previously-hidden failure
becoming visible, not a new fault. Expect to see errors from:

- an expired, revoked, or wrongly-scoped bot token (`invalid_auth`, `missing_scope`)
- messages addressed to a channel the bot has not been invited to
- rate limiting on large recipient sets (`ratelimited`) — the provider does not retry

### Added

- **`slack_user` data source** — looks up a single Slack user by `id` or `email` and
  exposes the full user object, including a nested `profile` block with display name,
  real name, title, phone, timezone, avatars, and account-status flags.
  - Exactly one of `id` or `email` must be set; violating this is caught at plan time,
    before any API call.
  - Lookup by `id` requires the `users:read` scope. Lookup by `email`, and population of
    the `email` attribute, additionally require **`users:read.email`** — a separate scope.
  - Without `users:read.email`, `profile.email` is `null` rather than an error; an
    *email lookup* without it fails with a diagnostic naming the scope to add.
  - Attributes Slack omits are `null`, not empty strings.
  - Note: `users.lookupByEmail` does not match deactivated accounts. Look those up by
    `id`, which returns them with `deleted = true`.

### Fixed

- Slack application errors (`ok: false`) are surfaced as Terraform diagnostics instead of
  being reported as success. Fixed at the shared request path, so it covers every API call
  the provider makes.
- A failure to reach Slack during refresh no longer removes messages from state. Read
  previously treated a network or auth failure identically to a deleted message, silently
  discarding state for messages that still existed and causing Terraform to post
  duplicates on the next apply. State is now dropped only when Slack positively confirms
  the message is gone (`thread_not_found`, `message_not_found`); anything else fails
  loudly and leaves state untouched.
- `terraform-plugin-docs` is now a proper tracked tool dependency. It was previously
  referenced only from `go:generate` comments, so `go mod tidy` would remove it and break
  documentation generation. Regenerating docs now requires `go generate -tags tools ./tools`.

### Unchanged

- `slack_user_ids` behaviour is unchanged and pinned by tests. It does gain error
  reporting from the fix above: a failing `users.list` call now surfaces instead of
  silently returning an empty map.

### Known issues

- `chat.update` and `chat.delete` are issued with `GET`, though Slack documents both as
  `POST`. Verified against a live workspace: both currently succeed, so this is a latent
  correctness issue rather than a functional one. Slack's Web API is permissive here today
  and could tighten without notice.
- Custom profile fields (`profile.fields`), `enterprise_user` (Enterprise Grid), and
  `locale` are not exposed by `slack_user`.
- The provider address is `pippiio.com/pippiio/slack`, a development address. Consuming it
  requires a `dev_overrides` CLI configuration; it is not resolvable from the public
  Terraform Registry.
