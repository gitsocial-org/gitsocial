# Testing Guide

GitSocial has comprehensive test coverage across all layers. This guide explains how to run, write, and debug tests.

## Test Architecture

```
E2E Tests (User Workflows)
    ↓ verify full integration
Component Tests (UI Units)
    ↓ verify rendering/interactions
Integration Tests (Extension Infrastructure)
    ↓ verify handlers/commands
Unit Tests (Core Business Logic)
    ↓ verify protocols/operations
```

## Test Types

### Unit Tests (Core Package)
- **Framework**: Vitest
- **Purpose**: Test core business logic, protocol operations, Git operations
- **Coverage**: 90.9% (2,613+ tests)
- **Speed**: ~5 seconds

### Component Tests (VSCode Package)
- **Framework**: Vitest + @testing-library/svelte + happy-dom
- **Purpose**: Test Svelte components in isolation
- **Coverage**: 23 tests across 2 components (growing)
- **Speed**: ~1.8 seconds

### Integration Tests (VSCode Package)
- **Framework**: @vscode/test-electron + Mocha
- **Purpose**: Test extension infrastructure (commands, handlers, messaging)
- **Coverage**: 8 test suites, 48 tests
- **Speed**: ~2-3 minutes per platform

### E2E Tests (VSCode Package)
- **Framework**: @vscode/test-electron + Mocha
- **Purpose**: Test complete user workflows in real VSCode
- **Coverage**: 6 tests (post creation, timeline, initialization)
- **Speed**: ~1 minute

## Running Tests

### Core Package

```bash
cd packages/core

pnpm test               # Run all tests
pnpm test:watch         # Watch mode
pnpm test:coverage      # With coverage report
```

### VSCode Package

```bash
cd packages/vscode

# Component tests (fast)
pnpm test:unit              # Run once
pnpm test:unit:watch        # Watch mode
pnpm test:unit:coverage     # With coverage

# Integration tests
pnpm test                   # Run integration tests

# E2E tests
pnpm test:e2e              # Run E2E workflows

# All tests
pnpm test:all              # Unit + Integration + E2E
```

## Coverage Thresholds

### Core Package
- Lines: 88%
- Functions: 97%
- Branches: 86%
- Statements: 88%
- **Current**: 91%+

### VSCode Components
- Lines: 84%
- Functions: 83%
- Branches: 65%
- Statements: 84%
- **Current**: 87%+

## Writing Tests

### Unit Tests (Core)

Test core business logic using Vitest:

```typescript
// packages/core/tests/social/post.test.ts
import { describe, it, expect } from 'vitest';
import { social } from './index';

describe('social.post', () => {
  it('creates post with content', async () => {
    const result = await social.post.createPost(workdir, 'Hello world!');
    expect(result.success).toBe(true);
  });

  it('returns error for empty content', async () => {
    const result = await social.post.createPost(workdir, '');
    expect(result.success).toBe(false);
    expect(result.error).toBeDefined();
  });
});
```

### Component Tests (VSCode)

Test Svelte components using Testing Library:

```typescript
// packages/vscode/tests/webview/components/PostCard.test.ts
import { render, screen } from '@testing-library/svelte';
import { describe, it, expect } from 'vitest';
import PostCard from './PostCard.svelte';

describe('PostCard', () => {
  const mockPost = {
    id: 'https://github.com/user/repo#commit:abc123456789',
    author: { name: 'Test User', email: 'test@example.com' },
    content: 'Hello world!',
    timestamp: new Date(),
    type: 'post' as const
  };

  it('renders post content', () => {
    render(PostCard, { props: { post: mockPost } });
    expect(screen.getByText('Hello world!')).toBeInTheDocument();
  });

  it('displays author name', () => {
    render(PostCard, { props: { post: mockPost } });
    expect(screen.getByText('Test User')).toBeInTheDocument();
  });
});
```

### Integration Tests (VSCode)

Test extension commands and handlers:

```typescript
// packages/vscode/tests/integration/suite/timeline.test.ts
import * as assert from 'assert';
import * as vscode from 'vscode';

describe('Timeline Integration', function() {
  this.timeout(10000);

  it('opens timeline view', async () => {
    await vscode.commands.executeCommand('gitsocial.openTimeline');
    await new Promise(resolve => setTimeout(resolve, 1000));

    const tabGroups = vscode.window.tabGroups.all;
    const hasTimelineView = tabGroups.some(group =>
      group.tabs.some(tab => tab.label === 'Timeline')
    );

    assert.ok(hasTimelineView, 'Timeline view should be open');
  });
});
```

### E2E Tests (VSCode)

Test complete user workflows:

```typescript
// packages/vscode/tests/integration/e2e-suite/post-workflow.test.ts
import * as assert from 'assert';
import * as vscode from 'vscode';

describe('E2E: Post Creation Workflow', function() {
  this.timeout(60000);

  it('should create and display post', async () => {
    await vscode.commands.executeCommand('gitsocial.initialize');
    await vscode.commands.executeCommand('gitsocial.createPost');
    // Verify post creation...
    assert.ok(true);
  });
});
```

## Test Patterns

### Result Type Testing

All operations return `Result<T>` for error handling:

```typescript
it('handles errors with Result type', async () => {
  const result = await social.post.createPost(workdir, invalidContent);

  if (!result.success) {
    expect(result.error).toBeDefined();
    expect(result.error.code).toBe('EXPECTED_ERROR_CODE');
    expect(result.error.message).toContain('expected message');
  }
});
```

### Async Operations

All cache and social operations are async:

```typescript
it('uses await for async operations', async () => {
  const posts = await social.post.getPosts(workdir, 'timeline');
  expect(posts.success).toBe(true);
});
```

### Component Props Testing

Test different prop combinations:

```typescript
it('renders compact layout when compact prop is true', () => {
  const { container } = render(PostCard, {
    props: { post: mockPost, compact: true }
  });
  expect(container.querySelector('.compact')).toBeInTheDocument();
});
```

## Configuration Files

### Core Package Tests

**`packages/core/tsconfig.json`** - Excludes test directory from build:
```json
{
  "exclude": ["node_modules", "tests", "../../build"]
}
```

**`packages/core/vitest.config.ts`** - Unit test configuration:
- Tests located in `tests/` directory
- Parallel execution with forks pool
- Coverage thresholds: 88/97/86/88

### Component Tests

**`packages/vscode/vitest.config.mts`** - Component test configuration:
- Svelte preprocessor with TypeScript support
- Happy-DOM environment (fast)
- Coverage thresholds: 84/83/65/84
- Includes: `tests/webview/**/*.test.ts`, `tests/unit/**/*.test.ts`

**`packages/vscode/tests/setup.ts`** - Test setup:
- VSCode API mocks
- Testing Library extensions

### Integration/E2E Tests

**`packages/vscode/tests/integration/suite/index.ts`** - Integration test runner
**`packages/vscode/tests/integration/runTest.ts`** - Test launcher
**`packages/vscode/tests/integration/e2e-suite/index.ts`** - E2E test runner
**`packages/vscode/tests/integration/runE2ETest.ts`** - E2E launcher

## CI/CD Testing

### GitHub Actions Workflow

Tests run automatically on all PRs and main branch:

**Core Package** (all platforms):
- Unit tests
- Coverage upload to Codecov (flag: `core`)
- Security audit (`pnpm audit`)

**VSCode Package** (all platforms):
- Component tests
- Integration tests
- Coverage upload to Codecov (flag: `vscode-components`)
- Linting

**E2E Tests** (Ubuntu only):
- Run on main branch and PRs
- Test results uploaded as artifacts on failure

### Platform Matrix

Tests run on:
- Ubuntu (latest)
- macOS (latest)
- Windows (latest)

Linux tests use `xvfb-run -a` for headless VSCode.

## Troubleshooting

### Component Tests Fail

**Problem**: `Cannot find module '@gitsocial/core'`
**Solution**: Build core package first:
```bash
cd packages/core && pnpm build
```

**Problem**: Svelte component not rendering
**Solution**: Check that `svelte-preprocess` is configured in `vitest.config.mts`

### Integration/E2E Tests Fail

**Problem**: Tests timeout on Linux
**Solution**: Ensure xvfb is installed:
```bash
sudo apt-get install xvfb
xvfb-run -a pnpm test
```

**Problem**: "Extension not found"
**Solution**: Build the extension first:
```bash
pnpm build
```

**Problem**: Tests hang indefinitely
**Solution**: Increase timeout in test file:
```typescript
this.timeout(60000); // 60 seconds
```

### Coverage Too Low

**Problem**: Coverage below thresholds
**Solution**:
1. View HTML report: `open coverage/index.html`
2. Identify uncovered lines
3. Add tests for critical paths
4. Adjust thresholds in config if needed

### Common Issues

**Flaky tests**: Add proper wait conditions instead of fixed timeouts
**Memory leaks**: Clean up test resources in `afterEach` hooks
**Slow tests**: Use component tests instead of integration tests where possible

## Test Data Management

### Test Fixtures

Create reusable test data:

```typescript
// test/fixtures/posts.ts
export const mockPost = {
  id: 'https://github.com/user/repo#commit:abc123456789',
  author: { name: 'Test User', email: 'test@example.com' },
  content: 'Test content',
  timestamp: new Date('2025-01-15T10:00:00Z'),
  type: 'post' as const
};
```

### Temporary Workspaces

E2E tests create temporary Git repositories:

```typescript
const workspaceDir = path.join(tmpdir(), `gitsocial-test-${Date.now()}`);
mkdirSync(workspaceDir, { recursive: true });
```

Clean up after tests:

```typescript
after(() => {
  if (fs.existsSync(workspaceDir)) {
    fs.rmSync(workspaceDir, { recursive: true });
  }
});
```

## Best Practices

### Test Naming

```typescript
// Good - descriptive, specific
it('returns error when content is empty')
it('displays author name from post data')
it('opens timeline view when command executed')

// Bad - vague
it('works correctly')
it('test post')
```

### Test Organization

- One test file per source file
- Group related tests in `describe` blocks
- Use `beforeEach`/`afterEach` for setup/cleanup
- Keep tests independent (no shared state)

### Test Coverage

Focus on:
1. Critical user paths (post creation, timeline)
2. Error handling (invalid input, network errors)
3. Edge cases (empty states, long content)
4. Integration points (Git operations, cache)

Don't test:
- Third-party libraries
- VSCode API (trust it works)
- Trivial getters/setters

### Performance

- Component tests: Keep under 2 seconds total
- Integration tests: Keep under 5 minutes per platform
- E2E tests: Keep under 10 minutes total
- Use `happy-dom` instead of `jsdom` for component tests (faster)

## Security Testing

### Automated Scanning

**Dependabot** (`.github/dependabot.yml`):
- Weekly dependency updates
- Grouped minor/patch updates
- Separate for core and vscode packages

**CodeQL** (`.github/workflows/codeql.yml`):
- Runs on push to main and PRs
- JavaScript/TypeScript SAST
- Weekly scheduled scans

### Manual Security Testing

Run security audit:
```bash
pnpm audit
pnpm audit --audit-level=high
```

## Related Documentation

- [CONTRIBUTING.md](CONTRIBUTING.md) - Setup and development workflow
- [ARCHITECTURE.md](ARCHITECTURE.md) - System design and architecture
- [PATTERNS.md](PATTERNS.md) - Code patterns and conventions
- [INTERFACES.md](INTERFACES.md) - Type reference and API documentation

## Quick Reference

```bash
# Core package
cd packages/core
pnpm test                    # Run unit tests
pnpm test:coverage           # With coverage

# VSCode package
cd packages/vscode
pnpm test:unit               # Component tests
pnpm test                    # Integration tests
pnpm test:e2e               # E2E tests
pnpm test:all               # All tests

# Debugging
pnpm test:unit:watch         # Watch component tests
pnpm test -- --grep "pattern" # Run specific integration tests

# Coverage
pnpm test:unit:coverage      # Component coverage
open coverage/index.html     # View report
```
