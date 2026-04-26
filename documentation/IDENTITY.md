# Identity Verification

How GitSocial decides whether a signed commit's author identity is trustworthy. The protocol-level rules live in [`specs/GITMSG.md` §3.2](../specs/GITMSG.md#32-identity-verification); this document covers the implementation: which sources are consulted, how results are cached, and what the operational knobs are.

## Overview

A signed commit is **verified** when its `(signing key, author email)` pair has been attested by an external authority. Verification is a property of the binding, not the individual commit — once a `(key, email)` binding is verified, every signed commit matching that binding is verified.

Unsigned commits are **unverified**. They are never rejected.

## Sources

GitSocial knows three external sources. A binding is verified when at least one *enabled* source affirms it. Sources attest independently — a non-affirmative response from one source is not evidence against another.

| Source | Endpoint | Defined by | Default | Trust |
|---|---|---|---|---|
| Forge GPG endpoint | `https://<host>/<user>.gpg` (GPG only) | Forge convention | **on** | Forge attests key + email |
| Forge commits API | `https://api.<host>/repos/.../commits/<sha>` (any sig) | Forge convention | **on** | Forge attests key + email, any signature format |
| Domain owner | `https://<domain>/.well-known/gitmsg-id.json` | GitMsg protocol | **off** | Domain owner attests key + email |

Only the domain-owner well-known endpoint is GitMsg protocol surface. The forge integrations are conventions adopted by this implementation to interoperate with existing forge attestation services. Other GitMsg implementations may consult different sources or omit forge integrations entirely; the protocol guarantees only the trust model, not the trust set.

### Why DNS is opt-in

The protocol bounds DNS attestation to *self-attestation*: a document at `<host>` can only vouch for emails at `<host>` (or its recognized mail-subdomain children). A malicious or compromised domain can therefore only attest its own users — it cannot impersonate users at other domains.

That makes the trust gap narrower than it sounds, but two concerns remain:

- **Compromised or hijacked domains.** If an attacker takes over `example.com` (account takeover, expired-domain reregistration, or HTTPS endpoint compromise), they can publish attestations for `*@example.com`. Identity follows hostname control — the same model TLS DV and DKIM use — but it's still trust you may not want to extend silently.
- **Typosquatting.** `alice@examp1e.com` correctly verified is technically true ("examp1e.com vouches for this signer") but visually misleading next to `alice@example.com`. The badge can't help; users have to read the email.

DNS is off by default so you opt in deliberately. To enable:

```
gitsocial settings set identity.dns_verification true
```

Or in the TUI: `Settings → Identity → identity.dns_verification`. When the flag is off, the verifier never fetches `/.well-known/gitmsg-id.json` and any cached DNS bindings are excluded from `IsVerified` lookups (toggling takes immediate effect — no cache clear needed).

## Forge API token

The forge commits API is rate-limited. Unauthenticated against GitHub, the limit is 60 requests/hour per IP. Authenticated, it's 5000/hour per user.

Token resolution order:

1. `GITHUB_TOKEN` environment variable
2. `GH_TOKEN` environment variable
3. `gh auth token` (the [GitHub CLI](https://cli.github.com)'s stored credential)
4. Unauthenticated

Set one of these before running `gitsocial fetch` against any meaningful number of repos. Without a token, verification degrades gracefully — failed lookups are cached as negatives with a 1h TTL and retried on the next fetch.

## Mail-subdomain fallback

Defined by the protocol — see [§3.2](../specs/GITMSG.md#32-identity-verification). For an email like `alice@mail.example.com` with no document at `mail.example.com`, this CLI tries `example.com` once. Recognized prefixes: `mail.`, `email.`, `smtp.`, `imap.`, `pop.`, `mx.`. The walk is bounded to one step and won't produce a bare TLD.

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

Each source records its result independently. A negative response from one source never blocks attempts against another — if `forge_gpg` says no for `alice@example.com`, the next verification call still tries `forge_api` and `dns`.

Switching repos or forks does not invalidate cached bindings: an attestation is about the `(key, email)` pair, not the repo it appeared in.

## Multi-forge hosting

When a repo is mirrored to multiple forges (e.g., `github.com/alice/foo` and `gitlab.com/alice/foo`), each fetched copy verifies independently against its own forge. A commit from the GitHub fetch produces a `forge_gpg` binding scoped to `github.com`; the same commit fetched from GitLab produces a separate row scoped to `gitlab.com`. They never overwrite each other.

`IsVerified(key, email)` returns true if any positive row exists across all sources. Conflicts (one forge verified, another not) resolve to verified — it only takes one trusted authority.

## Trust pluralism

Two GitSocial implementations consulting different source sets may show different verified/unverified verdicts for the same forge-hosted commit. That is intentional. The protocol guarantees that "verified" means "an authority I trust attested this binding" and "unverified" means "no authority I trust attested it." It does not promise that all implementations agree on the trust set — the same way TLS clients with different root stores can disagree about a certificate, or PGP clients with different keyrings can disagree about a signature.

## Operational checks

Quick verification that the system is working:

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
