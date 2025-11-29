# Contributing to GitSocial

Thank you for your interest in contributing! GitSocial is a decentralized open-source Git-native social network. This guide will help you get started.

## Prerequisites

- Node.js 20+
- pnpm 8+ (`npm install -g pnpm`)
- Git 2.30+

## Coding Standards

Follow patterns in [PATTERNS.md](PATTERNS.md). See [ARCHITECTURE.md](ARCHITECTURE.md) for system design, [TESTING.md](TESTING.md) for testing guide, and [START.md](START.md) for LLM development guide.

## Getting Started

From the repository root, install dependencies and build both packages:

```bash
# Core package
cd packages/core
pnpm install
pnpm build

# VSCode extension
cd ../vscode
pnpm install
pnpm build
```

Verify everything works:

```bash
# In packages/core
pnpm test

# In packages/vscode
pnpm test:unit
```

If all tests pass, you're ready to contribute! See [TESTING.md](TESTING.md) for comprehensive testing guide.

## Development Workflow

Make your changes in the relevant package, then rebuild and test:

```bash
# In packages/core or packages/vscode
pnpm build       # Rebuild after changes
pnpm test        # Run tests
pnpm lint        # Check code style
```

For faster iteration, use watch mode:

```bash
pnpm watch       # Auto-rebuild on file changes
pnpm test:watch  # Auto-run tests (core package only)
```

## Submitting Changes

1. **Create a branch** - Use descriptive names: `fix-avatar-loading`, `add-quote-feature`
2. **Make changes** - Follow [PATTERNS.md](PATTERNS.md) conventions
3. **Test locally** - In each modified package:
   ```bash
   pnpm build && pnpm test && pnpm lint
   ```
4. **Push and create PR** - Provide clear description of changes
5. **CI runs automatically** - Tests run on Ubuntu, macOS, and Windows (~5-10 minutes)

If CI fails, check the logs and fix issues locally before pushing updates.

## Getting Help

- Check [ARCHITECTURE.md](ARCHITECTURE.md) for system design questions
- Review [PATTERNS.md](PATTERNS.md) for code style questions
- See [TESTING.md](TESTING.md) for testing questions
- Open an issue for bugs or feature discussions
- Ask questions in pull requests

## Advanced: Local CI Testing

Run GitHub Actions workflows locally using [act](https://github.com/nektos/act):

```bash
act pull_request
```

Note: The `.actrc` file configures act to run core package tests only. VSCode tests require actual VS Code and won't run in Docker. For comprehensive testing, use the commands shown above.

## Command Reference

### Core Package (`packages/core`)

```bash
pnpm build              # Compile TypeScript
pnpm watch              # Compile in watch mode
pnpm test               # Run tests
pnpm test:watch         # Run tests in watch mode
pnpm test:coverage      # Run tests with coverage report
pnpm lint               # Check code style
pnpm lint:fix           # Auto-fix code style issues
pnpm type-check         # Type check without building
pnpm clean              # Clean build artifacts
```

### VSCode Extension (`packages/vscode`)

```bash
pnpm build              # Build extension
pnpm watch              # Build in watch mode
pnpm compile            # Compile TypeScript only
pnpm test:unit          # Run unit tests
pnpm test:unit:watch    # Run unit tests in watch mode
pnpm test:unit:coverage # Run unit tests with coverage
pnpm test               # Run integration tests (requires VS Code)
pnpm lint               # Check code style
pnpm lint:fix           # Auto-fix code style issues
pnpm type-check         # Type check without building
pnpm package            # Create .vsix package for local testing/installation
```

## Related Documentation

- [README.md](../README.md) - Project overview
- [ARCHITECTURE.md](ARCHITECTURE.md) - System design and decisions
- [PATTERNS.md](PATTERNS.md) - Code patterns and conventions
- [TESTING.md](TESTING.md) - Testing guide
- [INTERFACES.md](INTERFACES.md) - Type reference
- [START.md](START.md) - LLM development guide
