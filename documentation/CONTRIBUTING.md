# Contributing

How to build GitSocial, submit a pull request and report a bug.

[Get started](#get-started) · [Submit a pull request](#submit-a-pull-request) · [Report a bug or request a feature](#report-a-bug-or-request-a-feature)

## Get started

Forge issues and PRs are disabled on every mirror. GitSocial uses its own tools for collaboration.

1. Install GitSocial (see [Install](../README.md#install))
2. Fork the repository on any host (GitHub, GitLab, Codeberg, or self-hosted)
3. Clone your fork: `git clone https://your-host.com/you/gitsocial`
4. Build it with Go 1.25.8 or newer: `go build -o bin/ ./...`
5. Read [Architecture](ARCHITECTURE.md) for system design, packages, and cache layout

## Submit a pull request

```bash
git config core.hooksPath scripts/hooks   # install the gate hook, once per clone
git switch -c feature/my-change           # make changes, commit
scripts/check.sh --quick                  # the gate the hook runs on push

gitsocial review pr create \
  --base main \
  --head feature/my-change \
  "Short description of change"

git push origin feature/my-change         # push your branch
gitsocial push                            # push PR metadata
```

After your first push, request fork registration in the [Matrix room](https://matrix.to/#/!uZYlsFjjQgPmSBYJaY:matrix.org?via=matrix.org). A maintainer then runs `gitsocial fork add <your-fork-url>` and sees your PRs and issues.

See [Review](REVIEW.md) for the full cross-forge PR workflow.

## Report a bug or request a feature

```bash
gitsocial pm issue create "Bug: description"
gitsocial push
```

The issue lands in your own repository, and fork registration makes it visible to maintainers. For quick questions or discussion, use the same Matrix room.
