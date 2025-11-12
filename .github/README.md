# GitHub Actions Configuration

This directory contains CI/CD workflows and configuration for the GitSocial project.

## CI Workflows

- `ci.yml` - Main CI pipeline that runs on pull requests and pushes to main
  - **Test Core Package** - Runs tests and linting for @gitsocial/core
  - **Test VSCode Extension** - Runs tests and linting for the VSCode extension across Ubuntu, macOS, and Windows

## Running Workflows Locally

Use [act](https://github.com/nektos/act) to run GitHub Actions workflows locally:

```bash
act pull_request
```

### Configuration

The `.actrc` file in the repository root configures act with:
- Platform mapping for ubuntu-latest
- linux/amd64 architecture (required for M-series Macs)
- Job filter to run only the core package tests

### Limitations

When running locally with act:
- Only the **Test Core Package** job runs (VSCode matrix tests are skipped)
- macOS and Windows tests cannot run in Docker containers
- Some GitHub Actions features may not work identically to the actual CI environment

### Alternative: Run Tests Directly

For comprehensive local testing including VSCode extension tests:

```bash
# Core package
cd packages/core
pnpm test
pnpm lint

# VSCode extension
cd packages/vscode
pnpm test:unit
pnpm test        # Integration tests (requires VS Code)
pnpm lint
```
