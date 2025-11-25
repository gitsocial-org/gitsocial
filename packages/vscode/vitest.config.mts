import { defineConfig } from 'vitest/config';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import { resolve } from 'path';
import sveltePreprocess from 'svelte-preprocess';

export default defineConfig({
  plugins: [svelte({ hot: false, compilerOptions: { dev: false }, preprocess: sveltePreprocess() })],
  resolve: {
    alias: {
      '@gitsocial/core/client': resolve(__dirname, '../../build/core/client/index.js'),
      '@gitsocial/core/utils': resolve(__dirname, '../../build/core/utils/index.js'),
      '@gitsocial/core': resolve(__dirname, '../../build/core/index.js'),
      'vscode': resolve(__dirname, './tests/unit/handlers/helpers/__vscode-stub.ts'),
      'lru-cache': resolve(__dirname, './node_modules/lru-cache')
    }
  },
  test: {
    globals: true,
    environment: 'happy-dom',
    setupFiles: ['./tests/setup.ts'],
    include: ['tests/webview/**/*.test.ts', 'tests/unit/**/*.test.ts'],
    exclude: [],
    pool: 'threads',
    fileParallelism: true,
    isolate: true,
    maxConcurrency: 4,
    server: {
      deps: {
        inline: ['lru-cache']
      }
    },
    coverage: {
      provider: 'v8',
      reportsDirectory: '../../.test-artifacts/coverage/vscode',
      reporter: ['text', 'json', 'html', 'lcov'],
      include: ['src/webview/**/*.svelte', 'src/webview/**/*.ts', 'src/handlers/**/*.ts'],
      exclude: [
        'src/webview/**/types.ts',
        'src/webview/index.ts',
        'src/handlers/types.ts',
        'src/handlers/index.ts',
        '**/*.d.ts'
      ],
      thresholds: {
        lines: 84,
        functions: 83,
        branches: 65,
        statements: 84
      }
    }
  }
});
