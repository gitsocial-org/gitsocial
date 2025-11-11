"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
const config_1 = require("vitest/config");
exports.default = (0, config_1.defineConfig)({
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
//# sourceMappingURL=vitest.config.js.map