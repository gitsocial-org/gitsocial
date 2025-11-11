import { defineConfig } from 'vitest/config';

export default defineConfig({
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
