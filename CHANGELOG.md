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

`slack_user_ids` changes behaviour in the same spirit: a username it cannot resolve is now
an error rather than a silently missing map entry. A configuration carrying a stale or
mistyped username has been quietly losing that recipient, and will now say so. See
**Fixed** below.

### Added

- **`user_token` provider attribute** (`SLACK_USER_TOKEN`) — an optional Slack **user**
  token (`xoxp-…`) alongside the bot token. Required only to *manage* user groups: Slack
  refuses `usergroups.create` for bot tokens in workspaces that restrict who may manage
  user groups, answering `permission_denied` rather than a missing-scope error. Every
  other part of the provider, including the `slack_usergroup` data source, continues to
  use the bot token alone.

- **`slack_usergroup` resource and data source** — manage Slack user groups (`@mention`
  groups) with name, handle, purpose, default channels and membership.
  - The purpose attribute is Slack's `description` field, named after the group dialog in
    the Slack UI rather than after the API.
  - **The resource requires `user_token`**; the data source does not. Configuring the
    resource without one fails at plan time with a diagnostic showing exactly what to set.
  - **Requires a paid Slack plan.** User groups are unavailable on the free plan, where
    every `usergroups.*` call fails with `paid_only`. Requires `usergroups:read` and
    `usergroups:write`. Slack additionally gates group *creation* on a workspace setting,
    so a correctly-scoped token can still be refused with `permission_denied`.
  - **`terraform destroy` disables a group rather than deleting it.** Slack provides no
    delete for user groups, and a disabled group keeps its name and handle **reserved**.
    Re-creating a group with the same handle re-enables and adopts the disabled one, which
    is reported as a warning. Creating against an *active* handle fails instead, rather
    than silently taking over a group Terraform did not create.
  - **`users` is authoritative.** Slack offers only a replace operation for membership, so
    anyone added to a managed group by hand is removed on the next apply — and Slack sends
    no notification when that happens. **Omit `users`** to leave membership entirely to
    Slack; the provider then never touches it.
  - Groups synced from an identity provider (`is_idp_group`) or with membership locked
    (`is_membership_locked`) refuse membership writes with an explanatory diagnostic. Both
    flags are exposed so configuration can branch on them.
  - The data source finds disabled groups too, by `id` or `handle`.

- **`slack_user` data source** — looks up a single Slack user by `id`, `email` or `name`
  and exposes the full user object, including a nested `profile` block with display name,
  real name, title, phone, timezone, avatars, and account-status flags.
  - Exactly one of `id`, `email` or `name` must be set; violating this is caught at plan
    time, before any API call.
  - Lookup by `id` requires the `users:read` scope. Lookup by `email`, and population of
    the `email` attribute, additionally require **`users:read.email`** — a separate scope.
  - Without `users:read.email`, `profile.email` is `null` rather than an error; an
    *email lookup* without it fails with a diagnostic naming the scope to add.
  - Attributes Slack omits are `null`, not empty strings.
  - Workspace-defined custom profile fields are exposed as `profile.fields`, a map keyed by
    Slack's field ID with `value` and `alt` per entry. The keys are workspace-specific so
    they cannot be enumerated in the schema; map them to labels with Slack's
    `team.profile.get`. Null when Slack omits the key, an empty map when the user has none.
  - Note: `users.lookupByEmail` does not match deactivated accounts. Look those up by
    `id`, which returns them with `deleted = true`.
- **Lookup by username** — `slack_user` accepts `name`, the user's Slack handle, matched
  exactly and case-sensitively. This is the capability `slack_user_ids` provided, and it
  now returns the whole user rather than the ID alone.
  - Slack has no lookup-by-username endpoint, so this selector scans `users.list`. The
    scan is paginated and stops at the page holding the match. `users.list` is a Tier 2
    method (roughly 20 requests per minute), so prefer `id` or `email` where you have one
    and keep `for_each` sets over usernames small.
  - An unmatched username fails the apply with a diagnostic naming the handle.

### Deprecated

- **`slack_user_ids`** — superseded by `slack_user` with the `name` argument, and
  **scheduled for removal in v2.0.0**. Using it now emits a Terraform deprecation
  warning. It is still maintained for as long as it ships — the pagination and
  unresolved-username fixes under **Fixed** apply to it — so v1.x users are not required
  to migrate to get them.

  ```hcl
  # before
  data "slack_user_ids" "this" {
    usernames = ["u1", "u2"]
  }
  # data.slack_user_ids.this.slack_ids["u1"]

  # after
  data "slack_user" "this" {
    for_each = toset(["u1", "u2"])
    name     = each.value
  }
  # data.slack_user.this["u1"].id
  ```

  Note the cost difference: `slack_user_ids` resolved every username in one `users.list`
  call, whereas `for_each` over `slack_user` scans `users.list` once per username. For
  large sets, resolve the IDs once and keep them in a variable or local.

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
- **`slack_user_ids` no longer resolves usernames against a single page of `users.list`.**
  Slack paginates that endpoint; the provider read only the first page, so in any
  workspace larger than one page a perfectly valid username resolved to nothing. Combined
  with the silent-drop behaviour below, that removed real recipients from `slack_ids`
  without a word. The scan now follows the cursor to the end.
- **`slack_user_ids` fails on a username it cannot resolve, naming each one.** Previously
  such a username was dropped from `slack_ids` in silence. `slack_message` consumes that
  map as authoritative, so a shrunken result made the next apply **delete a message that
  had already been delivered** — a typo, a renamed account or a deactivated one was
  enough. The diagnostic lists only the usernames that failed.
- `slack_user_ids` also gains error reporting from the `ok: false` fix above: a failing
  `users.list` call now surfaces instead of silently returning an empty map.
- `terraform-plugin-docs` is now a proper tracked tool dependency. It was previously
  referenced only from `go:generate` comments, so `go mod tidy` would remove it and break
  documentation generation. Regenerating docs now requires `go generate -tags tools ./tools`.



### Known issues

- `chat.update` and `chat.delete` are issued with `GET`, though Slack documents both as
  `POST`. Verified against a live workspace: both currently succeed, so this is a latent
  correctness issue rather than a functional one. Slack's Web API is permissive here today
  and could tighten without notice.
- `enterprise_user` (Enterprise Grid) and `locale` are not exposed by `slack_user`.
- `slack_user_ids.last_updated` is the time of the read, so it changes on every plan and
  forces anything referencing it to change too. It is retained rather than removed because
  dropping an attribute would break configurations that read it; it goes away with the
  data source in v2.0.0.
- The provider address is `pippiio.com/pippiio/slack`, a development address. Consuming it
  requires a `dev_overrides` CLI configuration; it is not resolvable from the public
  Terraform Registry.
