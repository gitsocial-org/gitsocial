# Identity Verification

A signed commit is verified when its signing key and author email are attested as a pair by a source the reader trusts ([GITMSG.md §3.2](../specs/GITMSG.md#32-identity-verification)), and verified commits show a ⚿ badge next to the author.

[Sources](#sources) · [Commands](#commands) · [Reference](#reference)

## Sources

A binding is verified when at least one enabled source affirms it. Verification belongs to the pair: every commit signed with that key and email is verified. Sources are independent, and a negative from one does not count against another. Unsigned commits are unverified, and nothing rejects them.

| Source | Endpoint | Default |
|---|---|---|
| Forge GPG endpoint | `https://github.com/<user>.gpg`, GPG keys only | on |
| Forge commits API | `https://api.github.com/repos/<owner>/<repo>/commits/<sha>`, any signature format | on |
| Domain owner | `https://<domain>/.well-known/gitmsg-id.json`, the protocol's own source | off |

GitHub is the forge adapter shipped today. A repository on another host falls back to the domain source.

Domain attestation is off by default. Turn it on with `gitsocial settings set identity.dns_verification true`, or `d` in the TUI's Identity view. It applies at once, and cached domain bindings are ignored while it is off. A domain vouches for its own addresses and nothing else, so whoever controls the domain controls the badge. The badge does not tell `alice@examp1e.com` from `alice@example.com`; read the address.

## Commands

```
gitsocial id verify <commit>         # the commit's binding and the source that affirmed it
gitsocial id resolve <email>         # the domain document for an address
```

Signing needs `user.signingkey` and `gpg.format` in git config; SSH and GPG keys both work. The GitHub commits API allows 60 unauthenticated requests an hour and 5,000 with a token. Set `GITHUB_TOKEN` or `GH_TOKEN`, or run `gh auth login`, before fetching many repositories.

## Reference

| Topic | Rule |
|---|---|
| Mail subdomains | For `alice@mail.example.com` with no document at `mail.example.com`, `example.com` is tried once ([GITMSG.md §3.2](../specs/GITMSG.md#32-identity-verification)). Prefixes: `mail.`, `email.`, `smtp.`, `imap.`, `pop.`, `mx.` |
| Binding cache | `core_verified_bindings` ([ARCHITECTURE.md](ARCHITECTURE.md#schema)), per source: 24 hours for a verified result, 1 hour for a negative one |
| Cache scope | A binding is about the key and the email, so switching repositories or forks does not invalidate it |
| Several forges | Rows are scoped by forge host and do not overwrite each other. One affirmative row anywhere is enough |
| Disagreement | Two readers with different trusted sources can disagree about the same commit; the protocol does not require them to agree |
| TUI | The Identity view ([TUI-KEYS.md](TUI-KEYS.md#core-views)) shows your own binding and the source that verified it |
