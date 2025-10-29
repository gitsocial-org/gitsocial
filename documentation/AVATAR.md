# Avatar System Documentation

## Overview

The GitSocial avatar system stores avatars as native PNG files with CSS-based circular styling for optimal performance. Key features:

- Single 80px PNG storage per avatar (browser scales as needed)
- Multi-provider API integration: GitHub, Gitea (Codeberg, Forgejo), GitLab (self-hosted + public)
- Fallback chain: GitHub/Gitea/GitLab → Generated SVG (Gravatar opt-in for privacy)
- Privacy-first: No external calls by default (suitable for air-gapped environments)

## Configuration

```typescript
// VSCode setting (default: false for privacy)
gitsocial.enableGravatar: boolean
```

**Privacy**: Gravatar is disabled by default. When disabled, only git repository host API and generated avatars are used (no external Gravatar calls). Enable in Settings → Privacy for Gravatar fallback support.

## Core API

```typescript
// Core functions (return file paths)
getUserAvatar(avatarDir: string, email: string, remoteUrl?: string): Promise<string>
getRepositoryAvatar(avatarDir: string, repoUrl: string): Promise<string>

// VSCode wrapper (returns data URI)
getAvatar(type: 'user' | 'repo', identifier: string, options?: {size?: number; context?: string}): Promise<string>
```

## Storage

### File Naming

```
~/Library/Application Support/Code/User/globalStorage/gitsocial.gitsocial/avatars/
├── user_{md5_hash}.png                    # Permanent GitHub user avatars
├── repo_{md5_hash}.png                    # Permanent GitHub repo avatars
├── gravatar_{md5_hash}.png                # Gravatar fallbacks (1 hour cache, only if enabled)
└── temp_user_{hash}_{timestamp}.svg       # Temporary generated avatars
```

### Strategy

- **PNG**: Downloaded images (GitHub API, Gravatar if enabled)
- **SVG**: Generated letter/icon avatars only
- **Permanent**: GitHub avatars persist indefinitely
- **1-hour cache**: Gravatar fallbacks (allows retrying GitHub after rate limit, only if enabled)
- **Temporary**: Generated SVG avatars auto-delete after 5 minutes
- **Single Size**: Always 80px source, UI scales via CSS

## Avatar Sources

### User Avatars (Priority Order)

1. **GitHub noreply emails**: `{id}+{username}@users.noreply.github.com` → `https://avatars.githubusercontent.com/u/{id}?v=4&s=80`
2. **GitHub commit lookup**: `GET /repos/{owner}/{repo}/commits?author={email}` → extract `author.avatar_url`
3. **Gitea commit lookup**: `GET /api/v1/repos/{owner}/{repo}/commits?author={email}` → extract `author.avatar_url` (Codeberg, self-hosted)
4. **GitLab avatar API**: `GET /api/v4/avatar?email={email}` (for GitLab repositories, respects instance config)
5. **Gravatar** (disabled by default): `https://www.gravatar.com/avatar/{md5}?s=80&d=identicon` (1-hour cache, requires `gitsocial.enableGravatar: true`)
6. **Generated SVG**: First + Last name initials (or email first letter if name unavailable) + MD5 color (temporary)

### Repository Avatars (Priority Order)

1. **Provider Detection**: Auto-detect Git host (GitHub, GitLab, Gitea, Bitbucket, custom domains)
2. **Provider API**:
   - **GitHub**: `GET /repos/{owner}/{repo}` → `organization.avatar_url || owner.avatar_url` (supports `?s=80`)
   - **Gitea**: `GET /api/v1/repos/{owner}/{repo}` → `avatar_url || owner.avatar_url` (supports `?s=80`)
   - **GitLab**: `GET /api/v4/projects/{owner%2Frepo}` → `avatar_url` (handles relative paths)
   - **Bitbucket**: `GET /2.0/repositories/{owner}/{repo}` → `links.avatar.href`
3. **Unknown Provider Fallback**: Try GitHub → GitLab → Bitbucket APIs in sequence
4. **Local repo**: Home icon SVG for `identifier === 'myrepository'` (temporary)
5. **Generated SVG**: Repository initial + MD5 color (temporary)

**Note**: Both GitHub and Gitea support the `?s=80` size parameter for optimal quality. Size parameter is automatically added based on provider detection, not hostname matching.

### User Avatars Design Decision

**Current**: Privacy-first with GitHub + Gitea + GitLab user avatars

**Privacy approach:**
- **Default**: GitHub + Gitea + GitLab APIs + generated avatars (no external calls beyond repository host)
- **Opt-in Gravatar**: Available for users who want broader coverage
- **Air-gapped friendly**: Works without Gravatar in isolated environments

**Gitea support:**
- Uses `/api/v1/repos/{owner}/{repo}/commits?author={email}` endpoint (same as GitHub API)
- Works for Codeberg, projects.blender.org, and self-hosted Gitea/Forgejo instances
- Returns user avatar from commit history
- Supports size parameter `?s=80` for optimal quality

**GitLab support:**
- Uses `/api/v4/avatar?email=` endpoint (gitlab.com + self-hosted)
- Respects instance configuration (won't call Gravatar if disabled by admin)
- Works for users with public emails on GitLab
- Particularly valuable for enterprise/self-hosted instances

**Why not other providers?**
- **Bitbucket**: Likely requires authentication

**Conclusion**: GitHub + Gitea + GitLab with opt-in Gravatar balances privacy, air-gapped support, and coverage.

## Caching

### Configuration

```typescript
{
  maxMemoryEntries: 500,
  cacheExpiration: 30 * 24 * 60 * 60 * 1000, // 30 days
  apiRateLimit: 100, // ms between API calls
  tempFileCleanup: 5 * 60 * 1000 // 5 minutes
}
```

### Strategy

- **Memory**: LRU cache (500 entries)
- **File**: Permanent (GitHub) + temporary (fallbacks)
- **Cache Keys**: `user_{hash}.png`, `repo_{hash}.png` (no size parameter)
- **Promise Deduplication**: Prevents concurrent requests for same avatar

## GitHub Authentication

Automatically uses VSCode's GitHub authentication when available:
- **Silent detection** via `vscode.authentication.getSession('github')`
- **5,000 requests/hour** (authenticated) vs 60/hour (unauthenticated)
- **Zero configuration** - works if user signed into GitHub in VSCode
- **Auto-updates** on auth session changes

```typescript
social.avatar.setGitHubToken(token: string | null): void
```

Files: `packages/vscode/src/auth.ts`, `packages/vscode/src/avatar.ts`, `packages/core/src/social/avatar/service.ts`

## Component Integration

```svelte
<Avatar
  type="user"
  identifier={post.author.email}
  name={post.author.name}
  repository={post.repository}  <!-- Enables GitHub API lookup -->
  size={32}                     <!-- Browser scales from 80px -->
/>
```

### CSS Styling

```css
.avatar img {
  border-radius: 50%; /* Circular shape via CSS */
}
```

## Performance Features

- **Lazy Loading**: Intersection Observer (50px margin)
- **GitHub Auth**: Auto-detects VSCode GitHub session (5,000 vs 60 req/hour)
- **Rate Limiting**: 100ms between GitHub API calls
- **File Efficiency**: PNG for photos, SVG for generated graphics
- **Browser Scaling**: Single 80px source, CSS handles sizing
- **Smart Caching**: Gravatar 1-hour cache, generated SVG 5-min cleanup
- **Retry Strategy**: Gravatar cache expires to allow GitHub API retry after rate limits reset
