# Release Process

## Creating a Beta Release

### 1. Update Version

Edit `packages/vscode/package.json`:

```json
{
  "version": "0.1.0-beta.1"
}
```

Use format: `MAJOR.MINOR.PATCH-beta.N`

### 2. Commit and Tag

```bash
git add packages/vscode/package.json
git commit -m "Bump version to 0.1.0-beta.1"
git tag v0.1.0-beta.1
git push origin main
git push origin v0.1.0-beta.1
```

### 3. Wait for Workflow

- GitHub Actions will automatically build and create a release
- Check: https://github.com/gitsocial-org/gitsocial/actions
- Release appears at: https://github.com/gitsocial-org/gitsocial/releases

### 4. Verify Release

- Pre-release badge should show for beta versions
- .vsix file should be attached
- Release notes auto-generated from commits

## Creating a Stable Release

Same process, but version without `-beta`:

```json
{
  "version": "0.1.0"
}
```

```bash
git tag v0.1.0
git push origin v0.1.0
```

## Version Numbering

- **Beta releases**: `0.1.0-beta.1`, `0.1.0-beta.2`, etc.
- **Stable releases**: `0.1.0`, `0.2.0`, `1.0.0`

Follow [Semantic Versioning](https://semver.org/):
- MAJOR: Breaking changes
- MINOR: New features (backward compatible)
- PATCH: Bug fixes

## For Beta Testers

### Installing from GitHub Release

1. Go to [Releases](https://github.com/gitsocial-org/gitsocial/releases)
2. Find the desired version
3. Download the `.vsix` file
4. Open VSCode
5. Go to Extensions (Ctrl+Shift+X / Cmd+Shift+X)
6. Click "..." menu at top right
7. Select "Install from VSIX..."
8. Choose the downloaded .vsix file

### Updating to New Beta

1. Download new .vsix file
2. Install from VSIX (overwrites previous version)
3. Reload VSCode when prompted

### Reporting Issues

Include in bug reports:
- Extension version (from Extensions panel)
- VSCode version (Help > About)
- Operating system
- Steps to reproduce

## Troubleshooting

**Workflow fails to create release:**
- Check Actions logs for errors
- Ensure version in package.json matches git tag (without 'v' prefix)
- Verify tag was pushed: `git ls-remote --tags origin`

**Release created but no .vsix file:**
- Check if `pnpm package` command succeeded in workflow logs
- Verify .vsix file was created in `packages/vscode/` directory

**Can't install .vsix:**
- Ensure you have VSCode 1.74.0 or higher
- Try: Uninstall old version first, then install from VSIX
- Check VSCode Developer Tools (Help > Toggle Developer Tools) for errors
