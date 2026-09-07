# Style Guide

One register for everything in this repository, the one `README.md` and `specs/` already use: short sentences that say what, not why.

[Where things go](#where-things-go) · [Prose rules](#prose-rules) · [Help text](#help-text) · [Documentation](#documentation) · [Comments](#comments) · [Commits](#commits) · [Tests](#tests) · [Checks](#checks)

## Where things go

| Kind of text | Home | Not here |
|---|---|---|
| What a command does | `Short`, `Long`, flag help | design reasons, history |
| How to do a task | a guide doc (`SOCIAL.md`, `PM.md`, `REVIEW.md`) | mechanism, rationale |
| Exact values: keys, layouts, env vars, artifacts | a reference table | prose |
| Why something is the way it is | the commit body of the change; a constraint the next editor must respect gets one line at the point of constraint | longer comments, help text, subjects, a separate decisions file |
| What changed and when | the commit body | code comments, changelogs in code |
| A rule an implementation must follow | `specs/` | documentation |

## Prose rules

Apply to help, errors, TUI hints, site strings, log lines, guides, comments and commit messages.

- One idea per sentence, under 20 words.
- Imperative for instructions, present tense for behavior.
- No em-dashes. Use a comma, a colon, or a new sentence.
- Parentheses hold a literal or a short aside, never a second sentence.
- No intensifiers or narration: exactly, deliberately, silently, loudly, forever, by construction, honest, genuinely, precisely, the whole.
- "never" and "always" only in a rule, not in a description.
- Name a file, function or flag when the reader has to go there. Otherwise say it in words.
- Counts and sizes go in tables; defaults go in flag help; prose carries neither.
- No "we", no "note that", no rhetorical questions.

## Help text

| Field | Rule | Limit |
|---|---|---|
| `Short` | verb first, no period, no parenthetical | 50 characters |
| flag help | what the flag does; cobra prints the default | 60 characters, no parenthetical |
| `Long` | one paragraph of what, then examples; anything longer goes to the guide and is linked | 12 lines |
| error | one sentence naming the thing and the next action | one line |
| stderr hint | same as error, prefixed `gitsocial:` | one line |

Examples, before and after.

`Short`:

- `List memos (session + personal + project + inherited by default; external hidden)`
- `List memos across tiers`

Flag help:

- `Also fetch registered forks, followed repos and identity bindings (local viewing state; nothing mirror publishes depends on it)`
- `Also fetch forks, followed repos and identity bindings`

Hint (the original has an em-dash where `[em-dash]` stands):

- `gitsocial: multiple s3 remotes; pushing to "backup" [em-dash] set git config gitsocial.pushRemote <name> to choose`
- `gitsocial: several s3 remotes, pushing to "backup". Choose one with: gitsocial remote default <name>`

`Long` for `push`, 73 lines today:

```
Publish local GitMsg data to one or more remotes. On an s3 remote with
site.publish enabled, also publish the static site.

Remotes resolve in order: arguments, git config gitsocial.pushRemote,
then origin, or the first s3 remote when origin is not one. Diverged
gitmsg/* branches merge automatically. Diverged code branches fail
with a hint.

Examples:
  gitsocial push                # resolved remotes, data and site
  gitsocial push r2 backup      # named remotes, in order
  gitsocial push --site-only    # refresh the site, push no data
  gitsocial push --dry-run      # show what would be pushed

See documentation/S3.md for remotes and thin fork buckets.
```

The current text's explanations (the remote heuristic, the thin-fork escape hatch, why gitmsg branches merge cleanly) become guide steps and reference rows in `S3.md`; the rest goes.

## Documentation

Two kinds of doc. A file is a guide, a reference, or a guide that ends with a Reference section of tables.

| Kind | Purpose | Shape | Examples |
|---|---|---|---|
| Guide | do a task | steps and commands in the order a user meets them | `SOCIAL.md`, `PM.md`, `STATIC-SITE.md` |
| Reference | look a value up | tables: keys, flags, layouts, env vars, artifacts | `SETTINGS.md`, the Reference section of `STATIC-SITE.md` |

Rules:

- A guide sentence tells the reader what to type or what they will see. A sentence that starts with "because", "so that" or "the reason" belongs in the commit body, or goes.
- A reference row is one line. If it needs a paragraph, it is two rows or a guide sentence.
- Specs keep the RFC register and never reference this implementation.
- Every doc opens with one sentence saying what it covers; the site uses it as the page's description. One line of section links follows it, README style. The first section starts right after, with no other text before it.
- A term the doc coins is defined where it first appears.
- Link, do not repeat. One explanation lives in one place.

Example, the `commits/` bullet in `STATIC-SITE.md`, 300 words before the rewrite, becomes two things. The reasoning (why commits get no page of their own) stays in the commit that introduced the layer.

- Guide: "`commits/` lists the default branch's commits, 100 per page. Commits have no page of their own; each row links to the app's commit view."
- Reference: `| commits/<n>.html | sealed commits list page | no-cache |`

## Comments

| Place | Rule |
|---|---|
| file header | one line: `// file.go - what this file holds` |
| function | one line above each function saying what it does |
| inline | one line, only where the next lines are not obvious from the code |
| package doc | under 10 lines, only for a package with a non-trivial contract |
| struct field | trailing, one line, only for a unit or a sentinel |

Not in comments: why a design was chosen, what was tried, version history, threat models, invariants restated from another file, the same reasoning twice. When such a block comes out it becomes one of four things:

- a constraint the next editor must respect: one line at the point of constraint;
- how a mechanism works: a reference row in the owning doc;
- why this over the alternatives: the commit body;
- nothing, since git history keeps the deleted text.

Examples:

- `site_pages.go`, 89 lines of version history above a constant, becomes `sitePagesVersion = 18 // bump when a page head or its sealed markup changes`.
- `gs-core.js`, 12 lines above `fetchHTTP` on why 429 and 5xx retry, becomes `// fetchHTTP retries 429, 5xx and header timeouts with jittered backoff; transport errors fail at once.`
- `index.html`, a 17-line comment on each CSP directive, becomes no comment. The directive list is the documentation; the reasoning is in the commit that set the policy.

The same rules apply to Go, JS, CSS, HTML, shell and tests.

## Commits

- Subject: `Area: what changed`, under 72 characters, one change.
- Body: why, in two to six lines. Name the user-visible effect if there is one.
- A subject with "and" or a semicolon is two commits.

Example. Before, one commit with no body:

`S3 backend: bucket writes go out in parallel and retry transient faults, a first push publishes the whole site, and push maintenance reports progress`

After, three commits: `S3: upload bucket writes in parallel with retry`, with a body naming the concurrency default and the retried status codes; `S3: publish the whole site on a first push`; `Push: report maintenance progress`.

## Tests

- Names say the behavior: `TestPush_thinBucketRefusesSite`, not `TestThin3`.
- Assert on behavior and output, not on source text. A regex over a CSS or JS file tests the file, not the site.
- One shared fixture per shape. A test does not build a repo it does not need.
- A test that needs explaining gets one doc line, nothing more.

## Checks

`scripts/prose-check.sh` is stage 0 of `scripts/check.sh`. It counts em-dashes outside `specs/`, comment blocks over 3 lines in Go, JS, CSS and HTML outside package docs, `Short` over 50 characters, and flag help over 60 characters or containing a parenthesis, and fails when any count rises above `scripts/prose-baseline.txt`. After a sweep lowers a count, `--update` accepts the new baseline; `--list <rule>` prints the offending lines. Commit subjects over 72 characters in the pushed range fail outright.
