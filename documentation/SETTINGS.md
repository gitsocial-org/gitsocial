# Settings

User preferences live in the personal bare repo at `~/.config/gitsocial/personal/`, in `refs/gitmsg/core/config`, and sync across machines with `gitsocial personal sync`.

[Commands](#commands) · [Keys](#keys) · [Cross-machine sync](#cross-machine-sync) · [Environment](#environment)

## Commands

```
gitsocial settings list
gitsocial settings get <key>
gitsocial settings set <key> <value>     # the first set creates the personal repo
```

A value resolves from the environment when the key is environment-scoped, else from the personal repo, else from the default below.

## Keys

| Key | Type | Default | Meaning |
|---|---|---|---|
| `identity.dns_verification` | bool | `false` | trust `.well-known/gitmsg-id.json` for attestation ([IDENTITY.md](IDENTITY.md)) |
| `output.color` | enum | `auto` | `auto`, `always`, `never` |
| `display.show_email` | bool | `false` | show the author's email next to the name on cards |
| `display.theme` | enum | `auto` | `auto` detects the terminal background; `light`, `dark` |
| `log.level` | enum | `info` | `debug`, `info`, `warn`, `error` |
| `extensions.{social,pm,release,review,memo}` | bool | `true` | show the extension in the TUI sidebar |
| `fetch.parallel` | int | `4` | concurrent fetch workers |
| `fetch.timeout` | int | `30` | per-repository fetch timeout, in seconds |
| `fetch.auto.enabled` | bool | `false` | fetch periodically while the TUI is open |
| `fetch.auto.interval` | int | `300` | auto-fetch interval in seconds, minimum 60 |
| `fetch.auto.backoff` | bool | `true` | slow auto-fetch while idle; reset when new items arrive |
| `s3.concurrency` | int | `16` | concurrent uploads per s3 push; `GITSOCIAL_S3_CONCURRENCY` overrides |

`fetch.workspace_mode` is a per-repository map (`default` or `*`) written by the first-fetch prompt, not by `settings set`. The registry in `library/core/settings/scopes.go` defines the core keys; extensions keep their own user state in `refs/gitmsg/<ext>/config` of the same repo.

## Cross-machine sync

```
gitsocial personal init --remote git@example.com:me/gitsocial-personal.git
gitsocial personal sync [--push-only | --fetch-only]
gitsocial personal status
```

Repeat `init --remote` and `sync` on each machine. Sync pushes and fetches `refs/heads/*` and `refs/gitmsg/*`; the last write wins on the config ref.

## Environment

| Variable | Effect |
|---|---|
| `XDG_CONFIG_HOME` | config root, default `~/.config` |
| `GITSOCIAL_PERSONAL_REPO` | the personal repo path, default `<config>/gitsocial/personal` |
| `GITSOCIAL_PPROF` | `cpu`, `mem` or `trace`: profile this run to `/tmp/gitsocial-*` |
| `MEMO_SESSION_ID` | the active memo session id |
| `MEMO_SESSION_DIR` | the memo session directory, default `~/.cache/gitsocial/memo/session` |
