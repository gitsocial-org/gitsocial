# Why GitSocial

[Data Sovereignty](#data-sovereignty) · [Static Hosting](#static-hosting) · [Portability](#portability) · [AI Agent Memory](#ai-agent-memory) · [Verified Identity](#verified-identity) · [Alternatives](#alternatives)

## Data Sovereignty

*Ownership and privacy: you have a complete local copy of all of your data at any given time, and nobody else can read it unless you publish it.*

On a forge, a hosted service such as GitHub or GitLab, a clone carries only the code, while the issues, the pull requests, the reviews and the releases are rows in the database of the host.

GitSocial stores the issues, pull requests, releases, posts and memos of a project as commits on the message-only `gitmsg/*` branches of its repository. Because the data is part of the repository, a clone is a complete copy of all of it, and everything works offline: a person or an agent can read the issues, write a review or cut a release, and `gitsocial fetch` and `gitsocial push` are all that synchronization needs.

Nobody can read that data except the people you give the remote to. There is no host in between that indexes it, mines it or trains a model on it. Publishing is a separate step, to a host of your choice, and it is off until you turn it on.

GitSocial stores your data in the format of the [GitMsg](../specs/GITMSG.md) protocol, so it stays compatible with future tools. Even plain `git log --branches='gitmsg/*'` shows every issue, pull request, release, post and memo without GitSocial installed.

## Static Hosting

*Host your project on any provider you choose, private or public, pay for storage only, and never worry about running, patching or upgrading servers.*

Today a project needs a host, and a host is a server. Someone has to run it, patch it, upgrade it, defend it, or pay for it by the seat.

GitSocial uses an S3-compatible bucket, without the need for a server process, a database or a login system, to provide the three things a forge gives a project: a remote to fetch from and push to, a page that people can read, and a place for its issues, pull requests and releases.

You choose the provider. The remote works on any S3-compatible storage, verified so far on AWS S3, Cloudflare R2 and DigitalOcean Spaces, and on a machine of your own. One bucket can hold many repositories, and moving between providers is a copy of files, so the repository is its own backup, since anyone who has fetched it holds a complete copy.

The bucket is the remote: `gitsocial clone` reads it directly. A private bucket is one that only the people with its credentials can read, and a site published into it is as private as the bucket. `gitsocial push` by the bucket owner sends the repository to it. With site publishing turned on, the same push rebuilds a static site in the same bucket. A bucket with public reads, or a domain in front of it, is also the site. [S3.md](S3.md) describes the remote and [STATIC-SITE.md](STATIC-SITE.md) the site.

There is nothing to run, patch or upgrade, and a bucket of static files offers very little surface to attack. This is how the project itself works: [gitsocial.org](https://gitsocial.org) is the bucket site for this repository, and the GitHub repository is a mirror with issues and pull requests turned off.

## Portability

*Your project, its forks and its history can be on any host, and can change host at any time without losing anything.*

Today a project that changes hosts leaves its issues, pull requests and reviews behind, and each of its contributors is asked to sign up again on the new one.

GitSocial lets an organization move a project from one host to another, or keep projects on different hosts, and the history, the reviews and the contributors come along. The issues, pull requests and releases move with the repository, because they are part of it. A review follows each version of its pull request, so it survives a rebase, and a merged pull request records its merge head where any clone can find it after the branch is deleted.

Your contributors do not have to adopt anything for you to begin. `gitsocial import` reads the issues, pull requests and releases of a project on GitHub or GitLab into the `gitmsg/*` branches of your clone, so a project that has lived on a forge for years is in the GitMsg format in one command, and the import becomes a sync from then on. From that day the project has a complete copy of its data outside the host, and a way out that is ready before it is needed.

A fork can be on GitHub, GitLab or Codeberg, or in a bucket of its own. The maintainer registers the fork once. From then on its pull requests arrive with `gitsocial fetch` and appear next to the project's own, reviewed with the same tool and the same history. A fork in a bucket can be a thin one that borrows the objects of the project it forked, so it does not copy the full history. [REVIEW.md](REVIEW.md) walks through the flows and [S3.md](S3.md#thin-fork-buckets) describes thin forks.

## AI Agent Memory

*GitSocial makes your AI agent memories shareable with the team and portable across models, harnesses and platforms.*

AI coding agents accumulate knowledge about a project over time: its conventions, its decisions, and the thing that went wrong once and must not go wrong again. Today that knowledge is lost or locked in. Each agent keeps it in its own files in its own place, in a format of its own; the rest of the team never sees it, and a switch to another agent or model starts from zero.

A GitSocial memo is a short note that you or your agent writes when something is worth keeping, stored as a commit on the `gitmsg/memo` branch. Because it is a commit, it has an author, a history and a diff: it is reviewed like any other change, it reaches everyone who fetches, and it outlives any particular agent or model. A memo can be written at one of five tiers, from a session that is discarded with its work up to the project itself, and a memo that proves useful is promoted upward through them. A memo that turns out to be wrong can be retracted.

A project can also inherit the memos of other repositories, so an organization publishes its policies once. An agent then loads memos on demand, by task: one working on payments pulls the security policy of the organization and the industry standard it has to follow, and one working on the UI pulls the design system and the brand guide.

The [agent skill](https://github.com/gitsocial-org/gitsocial-agent-skill) gives an agent the same access to the issues, the pull requests and the releases as to memos. [MEMO.md](MEMO.md) explains the tiers and the workflows.

## Verified Identity

*Your name is tied to a key and a domain you control, not to an account someone else can close.*

On a forge, an author is an account, and the account belongs to the host. It can be suspended or closed, and the name on years of commits goes with it.

GitSocial ties an author to a signing key instead of an account, and checks the name and email of each signed commit against sources it trusts: by default the key endpoints of the forges, and for a project that has left them, a record at `/.well-known/gitmsg-id.json` on a domain the author controls. A record on your own domain cannot be closed. [IDENTITY.md](IDENTITY.md) lists the sources and the rules.

## Alternatives

*GitSocial keeps the data in the repository and removes dependencies, while the alternatives move the data or add a layer.*

### What GitSocial Does Not Do

- **Host repositories.** GitSocial is not a hosting service. You bring the remote: a forge, a server you run yourself or a bucket.
- **Run CI.** GitSocial does not run CI, and running CI is out of scope. A planned extension records the runs of your existing CI as signed commits, for provenance only.
- **Take pull requests from unregistered forks.** The maintainer has to register a fork before its pull requests arrive, and both sides have to run GitSocial.
- **Give per-person push rights.** Anyone who holds the credentials of a bucket can push to every branch of it. There is no branch protection, so a contributor works from a fork and a pull request.
- **Take issues or comments from the web page.** Nothing runs on the server, so a visitor cannot comment or open an issue from the page. That takes a clone and a push.
- **Keep a central index.** Projects are found through lists and registration, not through a search of every project.
- **Import from every forge.** The import adapters cover GitHub and GitLab, and the forge identity adapter covers GitHub.

### Comparison

The rows are for private projects. For git-bug, a tool on top of the host you use, "as the host" means the value of that host.

| | GitSocial | GitHub | GitLab | Codeberg | Fossil | git-bug | walgit | Radicle |
|---|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|
| **[Data Sovereignty](#data-sovereignty)** | | | | | | | | |
| You hold a complete copy of all of your project data | yes | code only | code only | code only | yes | issues only | code only | yes |
| Nobody can read your data unless you publish it | yes | no | no | no | yes | as the host | yes | yes |
| Work continues when the host or the network is down | yes | code only | code only | code only | yes | yes | code only | yes |
| The tool is open source | MIT | no | open core | GPL | BSD | GPL | MIT | MIT |
| **[Static Hosting](#static-hosting)** | | | | | | | | |
| Nothing to run, patch or upgrade | yes | yes | partly | yes | no | as the host | no | no |
| You pay for storage, not per seat | yes | no | no | free | yes | as the host | yes | yes |
| The project site is static files, with no process in front | yes | no | no | no | no | no | no | no |
| **[Portability](#portability)** | | | | | | | | |
| Leaving is a copy of files, with nothing lost | yes | no | no | no | yes | yes | yes | yes |
| Contributors keep the accounts they have | yes | no | no | no | no | yes | no | no |
| A fork on another host can send a pull request | yes | no | no | no | no | no | no | no |
| Imports from GitHub or GitLab and keeps syncing | yes | one time | one time | one time | code only | yes | code only | code only |
| **[AI Agent Memory](#ai-agent-memory)** | | | | | | | | |
| What your agents learn stays with the team | yes | as files | as files | as files | as files | no | as files | as files |
| Memory tiers from session to project, with promotion | yes | no | no | no | no | no | no | no |
| Policies inherited from other repositories | yes | partly | no | no | no | no | no | no |
| Memories load on demand by task | yes | no | no | no | no | no | no | no |
| **[Verified Identity](#verified-identity)** | | | | | | | | |
| Nobody can close your account or lock you out | yes | no | no | no | yes | as the host | yes | yes |
| Authorship is verified against a key you control | yes | by account | by account | by account | no | partly | no | yes |
| **[What GitSocial Does Not Do](#what-gitsocial-does-not-do)** | | | | | | | | |
| Anyone can open an issue from the web page | no | yes | yes | yes | yes | no | no | no |
| CI comes in the box | no | yes | yes | yes | no | no | no | no |
| Per-person push rights and branch protection | no | yes | yes | yes | yes | as the host | no | delegates |
| Projects are found through a central search | no | yes | yes | yes | no | no | no | partly |

### GitHub, GitLab and Codeberg

A hosted forge, whether [GitHub](https://github.com), [GitLab](https://gitlab.com) or [Codeberg](https://codeberg.org), keeps the issues, the pull requests and the reviews in the database of its server, and an export through the API is a snapshot in a format of the host. A contributor needs an account on the host, and a project that moves away leaves its history of discussion behind.

With Codeberg or a self-hosted GitLab or [Forgejo](https://forgejo.org), the server has a new owner, and the data is still in its database. ForgeFed, which federates Forgejo servers, does not change that either.

### Radicle

[Radicle](https://radicle.xyz) is the closest in spirit, and the difference is transport and identity. Radicle is a peer-to-peer network with its own protocol, where each participant runs a node and holds a Radicle identity. GitSocial uses the remote you have and the identity sources that exist, and needs no daemon.

### Fossil

[Fossil](https://fossil-scm.org) holds the same belief, that tickets and discussion belong in the repository, but it is its own version control system, not git.

### git-bug

[git-bug](https://github.com/git-bug/git-bug) is the same idea for issues alone, without the shared protocol, the timeline, the site and the remote.

### walgit

[walgit](https://github.com/tobi/walgit) is the same idea, a bucket, with a process in front of it, and it carries no issues or reviews.

---

The [README](../README.md) shows how to install GitSocial and import a project.
