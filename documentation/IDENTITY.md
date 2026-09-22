# Identity Verification

GitSocial verifies a signed commit when an enabled source attests its signing key and author email as a pair ([GITMSG.md §3.2](../specs/GITMSG.md#32-identity-verification)).

[Sources](#sources) · [Commands](#commands) · [Reference](#reference)

## Sources

A binding is the pair of a signing key and an author email, and one enabled source that attests a binding is enough to verify it. The verification applies to the pair: each commit that has that key and that email is verified and shows a ⚿ badge adjacent to the author.

Each source is independent: a negative result from one source has no effect on a different source. An unsigned commit is not verified, and GitSocial does not reject it.

| Source | Endpoint | Default |
|---|---|---|
| Forge GPG endpoint | `https://github.com/<user>.gpg`, GPG keys only | on |
| Forge commits API | `https://api.github.com/repos/<owner>/<repo>/commits/<sha>`, all signature formats | on |
| Domain source | `https://<domain>/.well-known/gitmsg-id.json`, the source that the protocol defines | off |

GitHub is the only forge adapter at this time, so a repository on a different host uses the domain source.

The domain source is off by default. To enable it, run `gitsocial settings set identity.dns_verification true`, or use the `d` key in the Identity view of the TUI; the change is immediate. When it is off, GitSocial ignores the domain bindings in the cache.

A domain attests only its own addresses, so the person who controls the domain controls the badge. Read the address: the badge does not show the difference between `alice@examp1e.com` and `alice@example.com`.

## Commands

```
gitsocial id verify <commit>         # the commit's binding and the source that attested it
gitsocial id resolve <email>         # the domain document for an address
```

To sign commits, set `user.signingkey` and `gpg.format` in the git config; SSH keys and GPG keys both work. The GitHub commits API allows 60 requests each hour without a token and 5,000 with a token. Before you fetch many repositories, set `GITHUB_TOKEN` or `GH_TOKEN`, or run `gh auth login`.

## Reference

| Topic | Rule |
|---|---|
| Mail subdomains | If `mail.example.com` has no document for `alice@mail.example.com`, GitSocial tries `example.com` one time ([GITMSG.md §3.2](../specs/GITMSG.md#32-identity-verification)). Prefixes: `mail.`, `email.`, `smtp.`, `imap.`, `pop.`, `mx.` |
| Binding cache | `core_verified_bindings` ([ARCHITECTURE.md](ARCHITECTURE.md#schema)), for each source: 24 hours for a verified result, 1 hour for a negative result |
| Cache scope | A binding applies to the key and the email, so a change of repository or fork does not remove it from the cache |
| Several forges | The forge host is the scope of each row, and rows do not replace each other; one row that attests the binding is enough |
| Different results | Two readers with different trusted sources can get different results for the same commit, and the protocol allows this |
| TUI | The Identity view ([TUI-KEYS.md](TUI-KEYS.md#core-views)) shows your binding and the source that attested it |
