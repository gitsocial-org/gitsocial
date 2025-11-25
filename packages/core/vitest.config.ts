import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    globals: true,
    environment: 'node',
    include: ['tests/**/*.test.ts'],
    pool: 'forks',
    fileParallelism: true,
    isolate: true,
    maxConcurrency: 4,
    coverage: {
      provider: 'v8',
      reportsDirectory: '../../.test-artifacts/coverage/core',
      reporter: ['text', 'json', 'html', 'lcov'],
      include: ['src/**/*.ts'],
      exclude: ['src/**/types.ts'],
      thresholds: {
        lines: 88,
        functions: 97,
        branches: 86,
        statements: 88
      }
    }
  }
});
