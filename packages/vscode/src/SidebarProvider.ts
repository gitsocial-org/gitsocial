import * as vscode from 'vscode';
import * as path from 'path';
import {
  social
} from '@gitsocial/core';
import type { ExtensionMessage, WebviewMessage } from './handlers';
import type { WebviewManager } from './WebviewManager';
import { getUnpushedCounts } from './utils/unpushedCounts';
import { getHandler } from './handlers/registry';

export class SidebarProvider implements vscode.WebviewViewProvider {
  public static readonly viewType = 'gitsocial.sidebar';
  private _view?: vscode.WebviewView;

  constructor(
    private readonly _extensionUri: vscode.Uri,
    private readonly _webviewManager: WebviewManager
  ) {}

  public resolveWebviewView(
    webviewView: vscode.WebviewView,
    _context: vscode.WebviewViewResolveContext,
    _token: vscode.CancellationToken
  ): void {
    this._view = webviewView;

    webviewView.webview.options = {
      enableScripts: true,
      localResourceRoots: [
        this._extensionUri,
        vscode.Uri.file(path.join(this._extensionUri.fsPath, 'out'))
      ]
    };

    webviewView.webview.html = this._getHtmlForWebview(webviewView.webview);

    // Handle messages from webview
    webviewView.webview.onDidReceiveMessage(
      (message: WebviewMessage) => {
        void this._handleMessage(message);
      }
    );

    // Handle visibility changes
    webviewView.onDidChangeVisibility(() => {
      if (webviewView.visible) {
        void this.refresh();
      }
    });
  }

  public async refresh(): Promise<void> {
    if (!this._view) {
      return;
    }

    // Update unpushed count when refreshing
    await this._updateUnpushedCount();
  }

  public createPost(content: string): void {
    // Open create post panel
    this._webviewManager.openPanel('createPost', 'Create Post', { content });
  }

  public postMessage(message: unknown): void {
    this._postMessage(message as ExtensionMessage);
  }

  private async _handleMessage(message: WebviewMessage): Promise<void> {
    switch (message.type) {
    case 'ready':
      // Send initial data
      await this._updateUnpushedCount();
      await this._handleGetLists();
      break;

    case 'openView':
      // Open a panel via WebviewManager
      if ('viewType' in message && 'title' in message) {
        if (message.type === 'openView') {
          this._webviewManager.openPanel(
            message.viewType,
            message.title,
            message.params
          );
        }
      }
      break;

    case 'refresh':
      await this._updateUnpushedCount();
      await this._handleGetLists();
      break;

    case 'list.getAll':
      await this._handleGetLists();
      break;

    case 'getUnpushedCounts':
      await this._updateUnpushedCount();
      break;

    case 'toggleZenMode':
      await vscode.commands.executeCommand('workbench.action.toggleZenMode');
      break;

    default: {
      const handler = getHandler(message.type);
      if (handler && this._view) {
        await handler(this._view as unknown as vscode.WebviewPanel, message);
      } else {
        console.warn('Unhandled sidebar message type:', message);
      }
      break;
    }
    }
  }

  async _handleGetLists(): Promise<void> {
    const repository = this._getRepository();
    if (!repository) {
      this._postMessage({ type: 'lists', data: [] });
      return;
    }

    const { git } = await import('@gitsocial/core');
    const isGitRepo = await git.isGitRepository(repository);
    if (!isGitRepo) {
      this._postMessage({ type: 'lists', data: [] });
      return;
    }

    try {
      const result = await social.list.getLists(repository);
      if (result.success && result.data) {
        this._postMessage({ type: 'lists', data: result.data });
      } else {
        console.error('Error getting lists:', result.error);
        this._postMessage({ type: 'lists', data: [] });
      }
    } catch (error) {
      console.error('Error getting lists:', error);
      this._postMessage({ type: 'lists', data: [] });
    }
  }

  private async _updateUnpushedCount(): Promise<void> {
    const repository = this._getRepository();
    if (!repository) {
      return;
    }

    const { git } = await import('@gitsocial/core');
    const isGitRepo = await git.isGitRepository(repository);
    if (!isGitRepo) {
      return;
    }

    try {
      // Use the shared utility to get unpushed counts
      const counts = await getUnpushedCounts(repository);

      this._postMessage({
        type: 'unpushedCounts',
        data: counts
      });
    } catch (error) {
      console.error('Error getting unpushed count:', error);
    }
  }

  private _postMessage(message: ExtensionMessage): void {
    if (this._view) {
      void this._view.webview.postMessage(message);
    }
  }

  private _getRepository(): string | undefined {
    const workspaceFolders = vscode.workspace.workspaceFolders;
    if (!workspaceFolders || workspaceFolders.length === 0) {
      return undefined;
    }

    return workspaceFolders[0].uri.fsPath;
  }

  private _getHtmlForWebview(webview: vscode.Webview): string {
    const scriptUri = webview.asWebviewUri(
      vscode.Uri.joinPath(this._extensionUri, 'webview.js')
    );

    const styleUri = webview.asWebviewUri(
      vscode.Uri.joinPath(this._extensionUri, 'webview.css')
    );

    const codiconUri = webview.asWebviewUri(
      vscode.Uri.joinPath(this._extensionUri, 'codicon.css')
    );

    const nonce = getNonce();

    return `<!DOCTYPE html>
      <html lang="en">
      <head>
        <meta charset="UTF-8">
        <meta http-equiv="Content-Security-Policy"
              content="default-src 'none'; style-src ${webview.cspSource} 'unsafe-inline';
                       script-src 'nonce-${nonce}'; font-src ${webview.cspSource} data:;">
        <meta name="viewport" content="width=device-width, initial-scale=1.0">
        <link href="${String(codiconUri)}" rel="stylesheet">
        <link href="${String(styleUri)}" rel="stylesheet">
        <title>GitSocial</title>
      </head>
      <body>
        <div id="sidebar"></div>
        <script nonce="${nonce}">
          const vscode = acquireVsCodeApi();
          window.vscode = vscode;
          window.viewType = 'sidebar';
        </script>
        <script nonce="${nonce}" src="${String(scriptUri)}"></script>
      </body>
      </html>`;
  }
}

function getNonce(): string {
  let text = '';
  const possible = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
  for (let i = 0; i < 32; i++) {
    text += possible.charAt(Math.floor(Math.random() * possible.length));
  }
  return text;
}
