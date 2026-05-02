# Identity Verification

## Overview

GitSocial considers a signed commit **verified** when its `(signing key, author email)` pair has been attested by an external authority. Verification is a property of the binding, not the individual commit: once a `(key, email)` binding is verified, every signed commit matching it is verified.

Unsigned commits are **unverified**. They are never rejected.

Protocol-level rules live in [`specs/GITMSG.md` §3.2](../specs/GITMSG.md#32-identity-verification); this doc covers the implementation.

## Sources

GitSocial knows three external sources. A binding is verified when at least one *enabled* source affirms it. Sources attest independently; a non-affirmative response from one is not evidence against another.

| Source | Endpoint | Defined by | Default | Trust |
|---|---|---|---|---|
| Forge GPG endpoint | `https://<host>/<user>.gpg` (GPG only) | Forge convention | **on** | Forge attests key + email |
| Forge commits API | `https://api.<host>/repos/.../commits/<sha>` (any sig) | Forge convention | **on** | Forge attests key + email, any signature format |
| Domain owner | `https://<domain>/.well-known/gitmsg-id.json` | GitMsg protocol | **off** | Domain owner attests key + email |

Only the domain-owner well-known endpoint is GitMsg protocol surface. The forge integrations are conventions this implementation adopts to interoperate with existing forge attestation services.

### Why DNS is opt-in

DNS attestation is self-attestation: a domain can only vouch for its own emails. Two concerns remain:

- **Compromised or hijacked domains.** If an attacker takes over `example.com` (account takeover, expired-domain reregistration, or HTTPS endpoint compromise), they can publish attestations for `*@example.com`. Identity follows hostname control (the same model TLS DV and DKIM use), but it's still trust you may not want to extend silently.
- **Typosquatting.** `alice@examp1e.com` correctly verified is technically true ("examp1e.com vouches for this signer") but visually misleading next to `alice@example.com`. The badge can't help; users have to read the email.

DNS is off by default. To enable:

```
gitsocial settings set identity.dns_verification true
```

Or in the TUI: `Settings → Identity → identity.dns_verification`. When off, the verifier doesn't fetch `/.well-known/gitmsg-id.json` and cached DNS bindings are excluded from `IsVerified`. Takes effect immediately.

## Forge API token

The GitHub commits API rate-limits unauthenticated requests to 60/hr (5000/hr authenticated); set `GITHUB_TOKEN`, `GH_TOKEN`, or run `gh auth login` ([GitHub CLI](https://cli.github.com)) before fetching against many repos.

## Mail-subdomain fallback

Defined by the protocol (see [§3.2](../specs/GITMSG.md#32-identity-verification)). For an email like `alice@mail.example.com` with no document at `mail.example.com`, this CLI tries `example.com` once. Recognized prefixes: `mail.`, `email.`, `smtp.`, `imap.`, `pop.`, `mx.`. The walk is bounded to one step and won't produce a bare TLD.

## Caching

Verified bindings are cached per source in `core_verified_bindings`. Schema:

```sql
CREATE TABLE core_verified_bindings (
    key_fingerprint TEXT NOT NULL,
    email           TEXT NOT NULL,
    source          TEXT NOT NULL,    -- 'forge_gpg' | 'forge_api' | 'dns'
    forge_host      TEXT NOT NULL,    -- '' for DNS
    forge_account   TEXT,
    verified        INTEGER NOT NULL,
    resolved_at     TEXT NOT NULL,
    PRIMARY KEY (key_fingerprint, email, source, forge_host)
);
```

TTLs:

- **Positive (verified) results**: 24 hours
- **Negative results**: 1 hour

A negative from one source never blocks attempts against another: if `forge_gpg` says no for `alice@example.com`, the next call still tries `forge_api` and `dns`.

Switching repos or forks does not invalidate cached bindings: an attestation is about the `(key, email)` pair, not the repo it appeared in.

## Multi-forge hosting

When a repo is mirrored to multiple forges (e.g., `github.com/alice/foo` and `gitlab.com/alice/foo`), each fetched copy verifies independently against its own forge. A commit from the GitHub fetch produces a `forge_gpg` binding scoped to `github.com`; the same commit fetched from GitLab produces a separate row scoped to `gitlab.com`. They never overwrite each other.

`IsVerified(key, email)` returns true if any positive row exists across all sources. Conflicts (one forge verified, another not) resolve to verified; one trusted authority is enough.

## Trust pluralism

Two GitSocial implementations consulting different source sets may show different verdicts for the same commit. That is intentional. "Verified" means "an authority I trust attested this binding"; "unverified" means "no authority I trust attested it." The protocol does not require implementations to agree on the trust set, the same way TLS clients with different root stores can disagree about a certificate.

## Operational checks

Quick checks:

```bash
# Verify a specific commit
gitsocial id verify HEAD

# Resolve a DNS-attested identity directly
gitsocial id resolve alice@example.com

# See cached bindings
sqlite3 ~/.cache/gitsocial/cache.db \
    "SELECT email, source, forge_host, verified, resolved_at FROM core_verified_bindings"

# See per-commit signing keys (M6 column)
sqlite3 ~/.cache/gitsocial/cache.db \
    "SELECT hash, author_email, signer_key FROM core_commits WHERE signer_key IS NOT NULL LIMIT 10"
```

The TUI's `Configuration → Identity` view shows your own `(signing key, email)` binding state and the source that verified it.
