import * as path from 'path';
import { runTests } from '@vscode/test-electron';

async function main(): Promise<void> {
  try {
    const extensionDevelopmentPath = path.resolve(__dirname, '../../../packages/vscode');
    const extensionTestsPath = path.resolve(__dirname, './suite/index');
    const extensionTestsEnv = {
      NODE_PATH: path.resolve(__dirname, '../../../packages/vscode/node_modules')
    };

    await runTests({
      extensionDevelopmentPath,
      extensionTestsPath,
      extensionTestsEnv,
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
