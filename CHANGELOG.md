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

**`slack_message.message` is now marked sensitive.**

Terraform prints it as `(sensitive value)` in plan and apply output instead of echoing
the text. The message body is frequently not public — an onboarding announcement, an
unreleased change — and it was previously rendered in full in every plan.

**What this means for you:** you can no longer read the message diff in plan output. If a
value derived from it feeds somewhere Terraform refuses to print sensitive data, wrap the
consumer in [`nonsensitive()`](https://developer.hashicorp.com/terraform/language/functions/nonsensitive).
This hides the text from **output only** — Terraform state still stores it in plain text
and must be treated as secret.

**A changed `message` is now reposted rather than edited in place.**

`slack_message` previously sent the new text with `chat.update`. An edited message stays
exactly where it sits in the conversation, so a recipient who has scrolled past never sees
it — and the call fails outright with `message_not_found` once the original message has
been deleted, leaving every subsequent apply stuck on the same error with no way forward.

Updating `message` now deletes the old message and posts a replacement, so it arrives as
the most recent message in the conversation. A delete that reports the message as already
gone counts as success, so a configuration wedged by the error above repairs itself on the
next apply.

**What this means for you:**

- editing `message` **re-notifies every recipient** — it is a new message, not an edit
- the `ts` in `msg_map` changes on every text change
- if someone replied in a thread on the old message, that thread stays with the deleted
  message; it does not move to the replacement

**`slack_user_ids` now fails on a username it cannot resolve.**

Previously a username with no matching Slack account was dropped from `slack_ids` in
silence. Because `slack_message` consumes that map as an authoritative set, the shrunken
result made the next apply **delete the message already delivered to that user** — a typo,
a renamed account, or a deactivated one was enough to destroy a message somebody had
received. The data source now fails and names every username it could not resolve.

**What this means for you:** a configuration listing a username that no longer exists will
error instead of quietly messaging a smaller audience. Correct or remove the username.

### Added

- **`host` now defaults to `https://slack.com`** and no longer has to be configured.
  Previously omitting it failed with "Missing Slack API Host". Set it only to reach Slack
  through a proxy or to point the provider at a stub; an explicit value, or `SLACK_HOST`,
  still wins.

- **`user_token` provider attribute** (`SLACK_USER_TOKEN`) — an optional Slack **user**
  token (`xoxp-…`) alongside the bot token. Required only to *manage* user groups: Slack
  refuses `usergroups.create` for bot tokens in workspaces that restrict who may manage
  user groups, answering `permission_denied` rather than a missing-scope error. Every
  other part of the provider, including the `slack_usergroup` data source, continues to
  use the bot token alone.

- **`slack_usergroup` resource and data source** — manage Slack user groups (`@mention`
  groups) with name, handle, description, default channels and membership.
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
  - Workspace-defined custom profile fields are exposed as `profile.fields`, a map keyed by
    Slack's field ID with `value` and `alt` per entry. The keys are workspace-specific so
    they cannot be enumerated in the schema; map them to labels with Slack's
    `team.profile.get`. Null when Slack omits the key, an empty map when the user has none.
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
- Messages are deleted and edited against the channel Slack delivered them to, rather than
  the Slack ID from configuration. `chat.delete` and `chat.update` were passed the
  `msg_map` key — for a DM that is a user ID, not the conversation the message lives in —
  so both targeted a channel that does not exist. `Read` already used the stored channel;
  the write paths now agree with it.
- A failed message delete is no longer discarded. `Update` ignored the error from
  `chat.delete`, so a delete Slack refused still removed the entry from state: the message
  stayed in the workspace with nothing tracking it. Both `Update` and `destroy` now report
  the failure and stop.
- A message already deleted by hand in Slack no longer wedges `apply` and `destroy`. The
  error codes that positively confirm the message is gone count as success, since the
  desired end state holds either way.
- `terraform-plugin-docs` is now a proper tracked tool dependency. It was previously
  referenced only from `go:generate` comments, so `go mod tidy` would remove it and break
  documentation generation. Regenerating docs now requires `go generate -tags tools ./tools`.

### Unchanged

- `slack_user_ids` resolution is otherwise unchanged and pinned by tests, apart from the
  unresolved-username failure described above. It also gains error reporting from the
  `ok: false` fix: a failing `users.list` call now surfaces instead of silently returning
  an empty map.

### Known issues

- `chat.update` and `chat.delete` are issued with `GET`, though Slack documents both as
  `POST`. Verified against a live workspace: both currently succeed, so this is a latent
  correctness issue rather than a functional one. Slack's Web API is permissive here today
  and could tighten without notice.
- `enterprise_user` (Enterprise Grid) and `locale` are not exposed by `slack_user`.
- The provider address is `pippiio.com/pippiio/slack`, a development address. Consuming it
  requires a `dev_overrides` CLI configuration; it is not resolvable from the public
  Terraform Registry.
