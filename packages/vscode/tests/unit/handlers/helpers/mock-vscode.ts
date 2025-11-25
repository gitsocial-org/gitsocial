import { vi } from 'vitest';
import type * as vscode from 'vscode';

export function createMockWorkspace() {
  return {
    workspaceFolders: [
      {
        uri: {
          fsPath: '/mock/workspace',
          scheme: 'file',
          authority: '',
          path: '/mock/workspace',
          query: '',
          fragment: '',
          with: vi.fn(),
          toJSON: vi.fn()
        },
        name: 'mock-workspace',
        index: 0
      }
    ] as vscode.WorkspaceFolder[],
    getConfiguration: vi.fn(),
    onDidChangeConfiguration: vi.fn(),
    onDidChangeWorkspaceFolders: vi.fn(),
    fs: {
      readFile: vi.fn(),
      writeFile: vi.fn(),
      delete: vi.fn(),
      createDirectory: vi.fn()
    }
  };
}

export function createMockConfiguration() {
  const config = new Map<string, any>();
  return {
    get: vi.fn((key: string, defaultValue?: any) => {
      return config.has(key) ? config.get(key) : defaultValue;
    }),
    update: vi.fn(async (key: string, value: any) => {
      config.set(key, value);
    }),
    has: vi.fn((key: string) => config.has(key)),
    inspect: vi.fn()
  };
}

export function createMockWindow() {
  return {
    showInformationMessage: vi.fn(),
    showErrorMessage: vi.fn(),
    showWarningMessage: vi.fn(),
    showQuickPick: vi.fn(),
    showInputBox: vi.fn(),
    withProgress: vi.fn((options, task) => {
      return task({
        report: vi.fn()
      });
    }),
    createWebviewPanel: vi.fn(),
    showTextDocument: vi.fn()
  };
}

export function createMockEnv() {
  return {
    openExternal: vi.fn(),
    clipboard: {
      writeText: vi.fn(),
      readText: vi.fn()
    }
  };
}

export function createMockUri(fsPath = '/mock/path') {
  return {
    fsPath,
    scheme: 'file',
    authority: '',
    path: fsPath,
    query: '',
    fragment: '',
    with: vi.fn(),
    toJSON: vi.fn(),
    toString: vi.fn(() => fsPath)
  };
}

export function mockVscodeModule() {
  const workspace = createMockWorkspace();
  const window = createMockWindow();
  const env = createMockEnv();
  const configuration = createMockConfiguration();
  workspace.getConfiguration = vi.fn(() => configuration as any);

  const defaultWorkspaceFolders = workspace.workspaceFolders;

  return {
    workspace: new Proxy(workspace, {
      get(target, prop) {
        return target[prop as keyof typeof target];
      },
      set(target, prop, value) {
        target[prop as keyof typeof target] = value;
        return true;
      }
    }),
    window,
    env,
    Uri: {
      file: vi.fn((path: string) => createMockUri(path)),
      parse: vi.fn((path: string) => createMockUri(path))
    },
    ProgressLocation: {
      Notification: 15,
      Window: 10,
      SourceControl: 1
    },
    ConfigurationTarget: {
      Global: 1,
      Workspace: 2,
      WorkspaceFolder: 3
    },
    __resetWorkspace: () => {
      workspace.workspaceFolders = defaultWorkspaceFolders;
    }
  };
}
