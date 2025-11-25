import * as path from 'path';
import { runTests } from '@vscode/test-electron';
import { tmpdir } from 'os';
import { mkdirSync, rmSync } from 'fs';

async function main(): Promise<void> {
  let testWorkspace: string | undefined;

  try {
    const extensionDevelopmentPath = path.resolve(__dirname, '../../');
    const extensionTestsPath = path.resolve(__dirname, './e2e-suite/index');
    const extensionTestsEnv = {
      NODE_PATH: path.resolve(__dirname, '../../node_modules')
    };
    const vscodeTestCachePath = path.resolve(__dirname, '../../../../.test-artifacts/vscode-test');
    mkdirSync(vscodeTestCachePath, { recursive: true });

    testWorkspace = path.join(tmpdir(), `gitsocial-e2e-${Date.now()}`);
    mkdirSync(testWorkspace, { recursive: true });

    // eslint-disable-next-line no-console
    console.log(`Created test workspace: ${testWorkspace}`);

    await runTests({
      extensionDevelopmentPath,
      extensionTestsPath,
      extensionTestsEnv,
      cachePath: vscodeTestCachePath,
      launchArgs: [
        testWorkspace,
        '--disable-extensions',
        '--disable-workspace-trust'
      ]
    });

    // eslint-disable-next-line no-console
    console.log('E2E tests completed successfully');
  } catch (err) {
    console.error('Failed to run E2E tests', err);
    process.exit(1);
  } finally {
    // Clean up temporary workspace
    if (testWorkspace) {
      try {
        // eslint-disable-next-line no-console
        console.log(`Cleaning up test workspace: ${testWorkspace}`);
        rmSync(testWorkspace, { recursive: true, force: true });
        // eslint-disable-next-line no-console
        console.log('Test workspace cleaned up successfully');
      } catch (cleanupError) {
        console.error('Failed to clean up test workspace:', cleanupError);
        // Don't fail the tests if cleanup fails
      }
    }
  }
}

void main();
