const esbuild = require('esbuild');
const sveltePlugin = require('esbuild-svelte');
const sveltePreprocess = require('svelte-preprocess');
const path = require('path');
const { spawn, execSync } = require('child_process');
const fs = require('fs');

const production = process.argv.includes('--production');
const watch = process.argv.includes('--watch');
const lint = process.argv.includes('--lint');
const lintFix = process.argv.includes('--lint-fix');

const buildDir = path.resolve(__dirname, '../../build/vscode');

/**
 * @type {import('esbuild').BuildOptions}
 */
const extensionConfig = {
  entryPoints: ['src/extension.ts'],
  bundle: true,
  outfile: path.join(buildDir, 'extension.js'),
  external: ['vscode', 'child_process', 'fs', 'path', 'crypto'],
  format: 'cjs',
  platform: 'node',
  target: 'node16',
  sourcemap: !production,
  minify: production,
  logLevel: 'info',
  define: {
    'process.env.NODE_ENV': production ? '"production"' : '"development"'
  },
  nodePaths: [path.resolve(__dirname, 'node_modules')],
  alias: {
    '@gitsocial/core/client': path.resolve(__dirname, '../../build/core/client/index.js'),
    '@gitsocial/core/utils': path.resolve(__dirname, '../../build/core/utils/index.js'),
    '@gitsocial/core': path.resolve(__dirname, '../../build/core/index.js')
  }
};

/**
 * @type {import('esbuild').BuildOptions}
 */
const webviewConfig = {
  entryPoints: ['src/webview/index.ts'],
  bundle: true,
  outfile: path.join(buildDir, 'webview.js'),
  external: ['lru-cache', 'child_process', 'fs', 'path', 'crypto'],
  format: 'iife',
  platform: 'browser',
  target: 'es2020',
  sourcemap: !production,
  minify: production,
  logLevel: 'info',
  loader: {
    '.woff': 'dataurl',
    '.woff2': 'dataurl',
    '.ttf': 'dataurl',
    '.eot': 'dataurl'
  },
  plugins: [
    sveltePlugin({
      preprocess: sveltePreprocess({
        typescript: true
      }),
      compilerOptions: {
        dev: !production,
        css: 'injected'
      }
    })
  ],
  define: {
    'process.env.NODE_ENV': production ? '"production"' : '"development"'
  },
  nodePaths: [path.resolve(__dirname, 'node_modules')],
  alias: {
    '@gitsocial/core/client': path.resolve(__dirname, '../../build/core/client/index.js'),
    '@gitsocial/core/utils': path.resolve(__dirname, '../../build/core/utils/index.js'),
    '@gitsocial/core': path.resolve(__dirname, '../../build/core/index.js')
  }
};

/**
 * @type {import('esbuild').BuildOptions}
 */
const cssConfig = {
  entryPoints: ['src/webview/styles/main.css'],
  bundle: true,
  outfile: path.join(buildDir, 'webview.css'),
  minify: production,
  logLevel: 'info',
  loader: {
    '.woff': 'file',
    '.woff2': 'file',
    '.ttf': 'file',
    '.eot': 'file'
  }
};

/**
 * Build the core module
 */
async function buildCore() {
  const corePath = path.resolve(__dirname, '../core');
  
  return new Promise((resolve, reject) => {
    console.log('📦 Building @gitsocial/core...');
    
    const buildProcess = spawn('pnpm', ['build'], {
      cwd: corePath,
      stdio: 'inherit',
      shell: true
    });

    buildProcess.on('close', (code) => {
      if (code === 0) {
        console.log('✅ Core build complete');
        resolve();
      } else {
        reject(new Error(`Core build failed with code ${code}`));
      }
    });

    buildProcess.on('error', (err) => {
      reject(err);
    });
  });
}

/**
 * Watch the core module for changes
 */
async function watchCore() {
  const corePath = path.resolve(__dirname, '../core');
  
  console.log('👀 Watching @gitsocial/core for changes...');
  
  const watchProcess = spawn('pnpm', ['watch'], {
    cwd: corePath,
    stdio: 'inherit',
    shell: true
  });

  watchProcess.on('error', (err) => {
    console.error('❌ Core watch failed:', err);
  });

  return watchProcess;
}

/**
 * Lint the core module
 */
async function lintCore() {
  const corePath = path.resolve(__dirname, '../core');
  
  return new Promise((resolve, reject) => {
    console.log('🔍 Linting @gitsocial/core...');
    
    const lintProcess = spawn('pnpm', ['lint'], {
      cwd: corePath,
      stdio: 'inherit',
      shell: true
    });

    lintProcess.on('close', (code) => {
      if (code === 0) {
        console.log('✅ Core lint complete');
        resolve();
      } else {
        reject(new Error(`Core lint failed with code ${code}`));
      }
    });

    lintProcess.on('error', (err) => {
      reject(err);
    });
  });
}

/**
 * Fix linting issues in the core module
 */
async function lintFixCore() {
  const corePath = path.resolve(__dirname, '../core');
  
  return new Promise((resolve, reject) => {
    console.log('🔧 Fixing lint issues in @gitsocial/core...');
    
    const lintFixProcess = spawn('pnpm', ['lint:fix'], {
      cwd: corePath,
      stdio: 'inherit',
      shell: true
    });

    lintFixProcess.on('close', (code) => {
      if (code === 0) {
        console.log('✅ Core lint fix complete');
        resolve();
      } else {
        reject(new Error(`Core lint fix failed with code ${code}`));
      }
    });

    lintFixProcess.on('error', (err) => {
      reject(err);
    });
  });
}

function ensureSymlink() {
  const symlinkPath = path.join(__dirname, 'out');
  const targetPath = buildDir;

  try {
    const stats = fs.lstatSync(symlinkPath);
    if (stats.isSymbolicLink()) {
      return;
    }
    fs.rmSync(symlinkPath, { recursive: true, force: true });
  } catch (err) {
    if (err.code !== 'ENOENT') {
      throw err;
    }
  }

  fs.symlinkSync(path.relative(path.dirname(symlinkPath), targetPath), symlinkPath);
}

function ensureNodeModulesSymlink() {
  const symlinkPath = path.join(buildDir, 'node_modules');
  const targetPath = path.join(__dirname, 'node_modules');

  try {
    const stats = fs.lstatSync(symlinkPath);
    if (stats.isSymbolicLink()) {
      return;
    }
    fs.rmSync(symlinkPath, { recursive: true, force: true });
  } catch (err) {
    if (err.code !== 'ENOENT') {
      throw err;
    }
  }

  fs.symlinkSync(path.relative(buildDir, targetPath), symlinkPath);
}

const FILES_TO_COPY = [
  { from: 'node_modules/@vscode/codicons/dist/codicon.css', to: 'codicon.css' },
  { from: 'node_modules/@vscode/codicons/dist/codicon.ttf', to: 'codicon.ttf' },
  { from: 'media', to: 'media' },
  { from: 'LICENSE', to: 'LICENSE' },
  { from: '.vscodeignore', to: '.vscodeignore' }
];

function copyStaticFiles() {
  FILES_TO_COPY.forEach(({ from, to }) => {
    const sourcePath = path.join(__dirname, from);
    const targetPath = path.join(buildDir, to);

    const stats = fs.statSync(sourcePath);
    if (stats.isDirectory()) {
      if (!fs.existsSync(targetPath)) {
        fs.mkdirSync(targetPath, { recursive: true });
      }
      const files = fs.readdirSync(sourcePath);
      for (const file of files) {
        fs.copyFileSync(
          path.join(sourcePath, file),
          path.join(targetPath, file)
        );
      }
    } else {
      fs.copyFileSync(sourcePath, targetPath);
    }
  });
}

function copyPackageJson() {
  const packageJsonPath = path.join(__dirname, 'package.json');
  const targetPath = path.join(buildDir, 'package.json');

  const packageJson = JSON.parse(fs.readFileSync(packageJsonPath, 'utf8'));
  packageJson.main = 'extension.js';

  fs.writeFileSync(targetPath, JSON.stringify(packageJson, null, 2));
}

function generateReadme() {
  const rootReadmePath = path.resolve(__dirname, '../../README.md');
  const vscodeRequirementsPath = path.join(__dirname, 'README.vscode.md');
  const targetPath = path.join(buildDir, 'README.md');

  let rootReadme = fs.readFileSync(rootReadmePath, 'utf8');
  const vscodeRequirements = fs.readFileSync(vscodeRequirementsPath, 'utf8');

  // Transform title to include "for VS Code"
  rootReadme = rootReadme.replace(/^# GitSocial/, '# GitSocial for VS Code');

  // Transform image paths to absolute GitHub URLs for marketplace compatibility
  rootReadme = rootReadme.replace(/documentation\/images\//g, 'https://github.com/gitsocial-org/gitsocial/raw/HEAD/documentation/images/');

  // Remove Installation section (between "## Installation" and next "##")
  rootReadme = rootReadme.replace(/## Installation\n\n[\s\S]*?(?=\n## )/m, '');

  // Insert Requirements after Quick Start section (before "## Documentation")
  rootReadme = rootReadme.replace(/(?=\n## Documentation)/, '\n' + vscodeRequirements + '\n');

  fs.writeFileSync(targetPath, rootReadme);
}

async function build() {
  try {
    fs.mkdirSync(buildDir, { recursive: true });
    ensureNodeModulesSymlink();

    if (lint) {
      // Lint both core and vscode
      await lintCore();
      
      console.log('🔍 Linting VSCode package...');
      const vscodeProcess = spawn('npm', ['run', 'lint:vscode'], {
        cwd: __dirname,
        stdio: 'inherit',
        shell: true
      });

      await new Promise((resolve, reject) => {
        vscodeProcess.on('close', (code) => {
          if (code === 0) {
            console.log('✅ All linting complete');
            resolve();
          } else {
            reject(new Error(`VSCode lint failed with code ${code}`));
          }
        });

        vscodeProcess.on('error', (err) => {
          reject(err);
        });
      });

      return;
    }

    if (lintFix) {
      // Fix lint issues in both core and vscode
      await lintFixCore();
      
      console.log('🔧 Fixing lint issues in VSCode package...');
      const vscodeProcess = spawn('npm', ['run', 'lint:fix:vscode'], {
        cwd: __dirname,
        stdio: 'inherit',
        shell: true
      });

      await new Promise((resolve, reject) => {
        vscodeProcess.on('close', (code) => {
          if (code === 0) {
            console.log('✅ All lint fixes complete');
            resolve();
          } else {
            reject(new Error(`VSCode lint fix failed with code ${code}`));
          }
        });

        vscodeProcess.on('error', (err) => {
          reject(err);
        });
      });

      return;
    }

    // Build core first (in both build and watch modes)
    await buildCore();

    if (watch) {
      // Start core watcher
      const coreWatcher = await watchCore();

      // Watch mode for extension
      const extContext = await esbuild.context(extensionConfig);
      const webContext = await esbuild.context(webviewConfig);
      const cssContext = await esbuild.context(cssConfig);

      await Promise.all([
        extContext.watch(),
        webContext.watch(),
        cssContext.watch()
      ]);

      copyStaticFiles();
      copyPackageJson();
      generateReadme();
      ensureSymlink();
      console.log('👀 Watching for changes in extension...');

      // Handle process termination
      process.on('SIGINT', () => {
        console.log('\n🛑 Stopping build processes...');
        coreWatcher.kill();
        process.exit(0);
      });
    } else {
      // Build mode
      // Compile test files first (this also compiles dependencies)
      console.log('📦 Compiling test files...');
      execSync('npx tsc --project tsconfig.test.json', {
        cwd: __dirname,
        stdio: 'inherit'
      });

      // Then bundle extension and webview (overwrites tsc output with bundled versions)
      await Promise.all([
        esbuild.build(extensionConfig),
        esbuild.build(webviewConfig),
        esbuild.build(cssConfig)
      ]);

      copyStaticFiles();
      copyPackageJson();
      generateReadme();
      if (!production) {
        ensureSymlink();
      }
      console.log('✅ Build complete');
    }
  } catch (err) {
    console.error('❌ Build failed:', err);
    process.exit(1);
  }
}

build();