# Notifications

Notifications aggregate events from all extensions into a single feed, sorted by timestamp (descending). Read state is tracked per-notification in `core_notification_reads`.

## Scopes

Where the notification data lives:

- **Workspace** — your own repo
- **Forks** — registered fork repos
- **Followed** — repos in your lists
- **Any** — any repo in the cache, regardless of relationship

All types exclude self-authored actions.

## Notification Types

| Source | Type | Scope | Trigger |
|--------|------|-------|---------|
| Core | `mention` | Any | Your email is @-mentioned in a commit message |
| Social | `comment` | Workspace + Followed | Someone comments on your post or a thread you participated in |
| Social | `repost` | Workspace + Followed | Someone reposts your post or a post in a thread you participated in |
| Social | `quote` | Workspace + Followed | Someone quotes your post or a post in a thread you participated in |
| Social | `follow` | Workspace | Someone follows your repository |
| PM | `issue-assigned` | Any | An issue is assigned to your email |
| PM | `issue-closed` | Any | An issue assigned to you is closed by someone else |
| PM | `issue-reopened` | Any | An issue assigned to you is reopened by someone else |
| Review | `fork-pr` | Forks | A non-draft PR is opened on a registered fork targeting your repo |
| Review | `review-requested` | Any | You are added as a reviewer on an open, non-draft PR |
| Review | `feedback` | Workspace + Any | Someone leaves feedback on a PR in your workspace or a PR you authored |
| Review | `approved` | Workspace + Any | Someone approves a PR in your workspace or a PR you authored |
| Review | `changes-requested` | Workspace + Any | Someone requests changes on a PR in your workspace or a PR you authored |
| Review | `pr-merged` | Any | A PR you authored is merged by someone else |
| Review | `pr-closed` | Any | A PR you authored is closed by someone else |
| Review | `pr-ready` | Forks | A draft PR on a registered fork is marked ready for review |
| Release | `new-release` | Followed | A repo in your lists publishes a release |
