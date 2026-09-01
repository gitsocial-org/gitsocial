# Settings

User preferences live in a bare git repo at `~/.config/gitsocial/personal/` (override with `GITSOCIAL_PERSONAL_REPO`; respects `$XDG_CONFIG_HOME`). Every editable key reads from and writes to `refs/gitmsg/core/config` there. The first `gitsocial settings set` auto-creates the repo — no separate init step.

Read order: env var (when env-scoped) → personal repo → registry default.

## Keys

| Key                                          | Type | Default | Notes                                                                                                 |
|----------------------------------------------|------|---------|-------------------------------------------------------------------------------------------------------|
| `identity.dns_verification`                  | bool | `false` | Trust DNS `.well-known/gitmsg-id.json` for attestation. Toggled in the TUI under `Identity → Policies`. |
| `output.color`                               | enum | `auto`  | `auto` / `always` / `never`.                                                                            |
| `display.show_email`                         | bool | `false` | Show author email alongside name on cards.                                                              |
| `display.theme`                              | enum | `auto`  | `auto` detects the terminal background; `light` / `dark` force it.                                      |
| `log.level`                                  | enum | `info`  | `debug` / `info` / `warn` / `error`.                                                                    |
| `extensions.{social,pm,release,review,memo}` | bool | `true`  | Show the extension in the TUI sidebar.                                                                  |
| `fetch.parallel`                             | int  | `4`     | Concurrent fetch workers.                                                                               |
| `fetch.timeout`                              | int  | `30`    | Per-repo fetch timeout (seconds).                                                                       |
| `fetch.auto.enabled`                         | bool | `false` | Periodically fetch in the TUI while it is open.                                                         |
| `fetch.auto.interval`                        | int  | `300`   | Auto-fetch interval in seconds (minimum 60).                                                            |
| `fetch.auto.backoff`                         | bool | `true`  | Slow auto-fetch when idle; reset to the base interval when new items arrive.                            |
| `s3.concurrency`                             | int  | `16`    | Concurrent object uploads per `s3://` push (overridden by `GITSOCIAL_S3_CONCURRENCY`).                  |

`fetch.workspace_mode` is a per-repo-URL map (`default` / `*` per repo URL), not a
scalar, so it lives outside the Registry and is read and written through
`LoadWorkspaceModes` / `WriteWorkspaceMode`.

The Registry (`library/core/settings/scopes.go`) is the source of truth for these *core* keys — adding one requires a code change. Extensions store their own user-level state in `refs/gitmsg/<ext>/config` inside the personal repo (same pattern as per-workspace ext config, just a different repo), syncing alongside core via `gitsocial personal sync`; each extension owns its keyspace and validation model.

## CLI

```
gitsocial settings list                  # all keys with current values
gitsocial settings get <key>
gitsocial settings set <key> <value>     # auto-inits personal repo on first call
```

## Sync

```bash
gitsocial settings set output.color never        # creates the personal repo
gitsocial personal init --remote git@example.com:me/gitsocial-personal.git
gitsocial personal sync                          # push (and fetch)
```

On a second host, repeat `personal init --remote …` + `personal sync` — values appear. Push+fetches `refs/heads/*` and `refs/gitmsg/*` against `origin`; last write wins on the config ref.

`gitsocial personal status` shows the repo path, init state, and remote.

## Environment variables

| Variable                  | Effect                                                              |
|---------------------------|---------------------------------------------------------------------|
| `XDG_CONFIG_HOME`         | User-config root (default `~/.config`).                             |
| `GITSOCIAL_PERSONAL_REPO` | Override personal-repo path (default `<config>/gitsocial/personal`).|
| `GITSOCIAL_PPROF`         | `cpu` / `mem` / `trace` — profile this run to `/tmp/gitsocial-*`.   |
| `MEMO_SESSION_ID`         | Pin the active memo session id (default: auto-generated per process).|
| `MEMO_SESSION_DIR`        | Override the memo session-repos directory (default `~/.cache/gitsocial/memo/session`). |
