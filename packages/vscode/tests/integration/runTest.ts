import * as path from 'path';
import { runTests } from '@vscode/test-electron';
import { mkdirSync } from 'fs';

async function main(): Promise<void> {
  try {
    const extensionDevelopmentPath = path.resolve(__dirname, '../../');
    const extensionTestsPath = path.resolve(__dirname, './suite/index');
    const extensionTestsEnv = {
      NODE_PATH: path.resolve(__dirname, '../../node_modules')
    };
    const vscodeTestCachePath = path.resolve(__dirname, '../../../../.test-artifacts/vscode-test');
    mkdirSync(vscodeTestCachePath, { recursive: true });

    await runTests({
      extensionDevelopmentPath,
      extensionTestsPath,
      extensionTestsEnv,
      cachePath: vscodeTestCachePath,
      launchArgs: [
        path.resolve(__dirname, './test-workspace')
      ]
    });
  } catch (err) {
    console.error('Failed to run tests', err);
    process.exit(1);
  }
}

void main();
