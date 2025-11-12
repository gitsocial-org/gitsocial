import { defineConfig } from 'vitest/config';
import { resolve } from 'path';

export default defineConfig({
  resolve: {
    alias: {
      '@gitsocial/core/client': resolve(__dirname, '../../build/core/client/index.js'),
      '@gitsocial/core/utils': resolve(__dirname, '../../build/core/utils/index.js'),
      '@gitsocial/core': resolve(__dirname, '../../build/core/index.js')
    }
  },
  test: {
    globals: true,
    environment: 'node',
    include: ['src/**/*.test.ts'],
    exclude: ['src/test/**/*'],
    coverage: {
      provider: 'v8',
      reporter: ['text', 'json', 'html'],
      include: ['src/webview/**/*.ts'],
      exclude: [
        'src/webview/**/*.test.ts',
        'src/webview/**/types.ts',
        'src/webview/index.ts',
        'src/webview/stores.ts'
      ]
    }
  }
});
