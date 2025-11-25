/* eslint-disable @typescript-eslint/naming-convention */
/* eslint-disable @typescript-eslint/no-unsafe-assignment */
/* eslint-disable @typescript-eslint/no-unsafe-call */
/* eslint-disable @typescript-eslint/no-unsafe-member-access */
/* eslint-disable @typescript-eslint/no-unsafe-return */
import * as path from 'path';
import { createRequire } from 'module';

export function run(): Promise<void> {
  const testsRoot = path.resolve(__dirname);
  const repositoryRoot = path.resolve(__dirname, '../../../../..');
  const requireFromExtension = createRequire(path.join(repositoryRoot, 'packages/vscode/package.json'));

  const MochaConstructor = requireFromExtension('mocha');
  const mocha = new MochaConstructor({
    ui: 'bdd',
    color: true,
    timeout: 60000
  });

  Object.assign(global, {
    describe: mocha.suite.constructor.prototype.describe ||
      ((title: string, fn: () => void) => mocha.suite.suite(title, fn)),
    it: mocha.suite.constructor.prototype.it ||
      ((title: string, fn: () => void) => mocha.suite.test(title, fn)),
    before: mocha.suite.constructor.prototype.before ||
      ((fn: () => void) => mocha.suite.beforeAll(fn)),
    after: mocha.suite.constructor.prototype.after ||
      ((fn: () => void) => mocha.suite.afterAll(fn)),
    beforeEach: mocha.suite.constructor.prototype.beforeEach ||
      ((fn: () => void) => mocha.suite.beforeEach(fn)),
    afterEach: mocha.suite.constructor.prototype.afterEach ||
      ((fn: () => void) => mocha.suite.afterEach(fn))
  });

  return new Promise((resolve, reject) => {
    try {
      mocha.addFile(path.resolve(testsRoot, 'post-workflow.test.js'));

      mocha.run((failures: number) => {
        if (failures > 0) {
          reject(new Error(`${failures} E2E tests failed.`));
        } else {
          resolve();
        }
      });
    } catch (err) {
      console.error(err);
      reject(err);
    }
  });
}
