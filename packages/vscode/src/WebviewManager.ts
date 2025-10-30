import * as vscode from 'vscode';
import * as path from 'path';
import { getHandler } from './handlers/registry';
import {
  gitMsgUrl,
  log
} from '@gitsocial/core';
import { getAvatar } from './avatar';

// Import handlers for side effects (registers all handlers)
import './handlers/index';
// Import types
import type { ExtensionMessage, WebviewMessage } from './handlers/types';

// Helper function to create panel icons using simple SVG representations
const createPanelIcon = (iconType: 'repo' | 'list' | 'settings' | 'edit' | 'timeline' | 'home' | 'search' | 'bell' | 'compass' | 'gitsocial'): vscode.Uri => {
  let svgContent: string;

  switch (iconType) {
  case 'repo':
    svgContent = '<svg width="16" height="16" viewBox="0 0 16 16" xmlns="http://www.w3.org/2000/svg"><path fill="#cccccc" d="M6 7H5V6H6V7ZM5 16H5.28L6.5 14.49L7.72 16H8V13H5V16ZM6 4H5V5H6V4ZM14 2.5V14.5L13.5 15H9V14H13V12H3.74C3.64 12 3.541 12.02 3.45 12.06C3.269 12.135 3.125 12.279 3.05 12.46C3.018 12.553 3.001 12.651 3 12.75V13.25C3.001 13.349 3.018 13.447 3.05 13.54C3.125 13.721 3.269 13.865 3.45 13.94C3.541 13.98 3.64 14 3.74 14H4V15H3.74C3.511 14.997 3.284 14.953 3.07 14.87C2.645 14.688 2.307 14.347 2.13 13.92C2.042 13.708 1.998 13.48 2 13.25V3.75C2.004 3.537 2.048 3.327 2.13 3.13C2.212 2.909 2.338 2.707 2.499 2.535C2.66 2.363 2.855 2.226 3.07 2.13C3.284 2.047 3.511 2.002 3.74 2H13.5L14 2.5ZM13 3H4V11H13V3ZM6 8H5V9H6V8Z"/></svg>';
    break;
  case 'list':
    svgContent = '<svg width="16" height="16" viewBox="0 0 16 16" xmlns="http://www.w3.org/2000/svg"><circle fill="#cccccc" cx="3" cy="4" r="1"/><rect fill="#cccccc" x="6" y="3.5" width="8" height="1"/><circle fill="#cccccc" cx="3" cy="8" r="1"/><rect fill="#cccccc" x="6" y="7.5" width="8" height="1"/><circle fill="#cccccc" cx="3" cy="12" r="1"/><rect fill="#cccccc" x="6" y="11.5" width="8" height="1"/></svg>';
    break;
  case 'settings':
    svgContent = '<svg width="16" height="16" viewBox="0 0 16 16" xmlns="http://www.w3.org/2000/svg"><path fill="#cccccc" d="M9.1 4.4L8.6 2H7.4l-.5 2.4-.7.3-2-1.3-.9.8 1.3 2-.2.7-2.4.5v1.2l2.4.5.3.8-1.3 2 .8.8 2-1.3.8.3.4 2.3h1.2l.5-2.4.8-.3 2 1.3.8-.8-1.3-2 .3-.8 2.3-.4V7.4l-2.4-.5-.3-.8 1.3-2-.8-.8-2 1.3-.7-.2zM9.4 1l.5 2.4L12 2.1l2 2-1.4 2.1 2.4.4v2.8l-2.4.5L14 12l-2 2-2.1-1.4-.5 2.4H6.6l-.5-2.4L4 13.9l-2-2 1.4-2.1L1 9.4V6.6l2.4-.5L2.1 4l2-2 2.1 1.4.4-2.4h2.8zm.6 7c0 1.1-.9 2-2 2s-2-.9-2-2 .9-2 2-2 2 .9 2 2zM8 9c.6 0 1-.4 1-1s-.4-1-1-1-1 .4-1 1 .4 1 1 1z"/></svg>';
    break;
  case 'edit':
    svgContent = '<svg width="16" height="16" viewBox="0 0 16 16" xmlns="http://www.w3.org/2000/svg"><path fill="#cccccc" d="M13.23 1h-1.46L3.52 9.25l-.16.22L1 13.59 2.41 15l4.12-2.36.22-.16L15 4.23V2.77L13.23 1zM2.41 13.59l1.51-3 1.45 1.45-2.96 1.55zm3.83-2.06L4.47 9.76l8-8 1.77 1.77-8 8z"/></svg>';
    break;
  case 'timeline':
    svgContent = '<svg width="16" height="16" viewBox="0 0 16 16" xmlns="http://www.w3.org/2000/svg"><path fill="#cccccc" fill-rule="evenodd" clip-rule="evenodd" d="M4 11.29l1-1v1.42l-1.15 1.14L3 12.5V10H1.5L1 9.5v-8l.5-.5h12l.5.5V6h-1V2H2v7h1.5l.5.5v1.79zM10.29 13l1.86 1.85.85-.35V13h1.5l.5-.5v-5l-.5-.5h-8l-.5.5v5l.5.5h3.79zm.21-1H7V8h7v4h-1.5l-.5.5v.79l-1.15-1.14-.35-.15z"/></svg>';
    break;
  case 'home':
    svgContent = '<svg width="16" height="16" viewBox="0 0 16 16" xmlns="http://www.w3.org/2000/svg"><path fill="#cccccc" fill-rule="evenodd" clip-rule="evenodd" d="M8.36 1.37l6.36 5.8-.71.71L13 6.964v6.526l-.5.5h-3l-.5-.5v-3.5H7v3.5l-.5.5h-3l-.5-.5V6.972L2 7.88l-.71-.71 6.35-5.8h.72zM4 6.063v6.927h2v-3.5l.5-.5h3l.5.5v3.5h2V6.057L8 2.43 4 6.063z"/></svg>';
    break;
  case 'search':
    svgContent = '<svg width="16" height="16" viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg"><path fill="#cccccc" d="M15.25 0a8.25 8.25 0 0 0-6.18 13.72L1 22.88l1.12 1 8.05-9.12A8.251 8.251 0 1 0 15.25.01V0zm0 15a6.75 6.75 0 1 1 0-13.5 6.75 6.75 0 0 1 0 13.5z"/></svg>';
    break;
  case 'bell':
    svgContent = '<svg width="16" height="16" viewBox="0 0 16 16" xmlns="http://www.w3.org/2000/svg"><path fill="#cccccc" d="M13.377 10.573a7.63 7.63 0 0 1-.383-2.38V6.195a5.115 5.115 0 0 0-1.268-3.446 5.138 5.138 0 0 0-3.242-1.722c-.694-.072-1.4 0-2.07.227-.67.215-1.28.574-1.794 1.053a4.923 4.923 0 0 0-1.208 1.675 5.067 5.067 0 0 0-.431 2.022v2.2a7.61 7.61 0 0 1-.383 2.37L2 12.343l.479.658h3.505c0 .526.215 1.04.586 1.412.37.37.885.586 1.412.586.526 0 1.04-.215 1.411-.586s.587-.886.587-1.412h3.505l.478-.658-.586-1.77zm-4.69 3.147a.997.997 0 0 1-.705.299.997.997 0 0 1-.706-.3.997.997 0 0 1-.3-.705h1.999a.939.939 0 0 1-.287.706zm-5.515-1.71l.371-1.114a8.633 8.633 0 0 0 .443-2.691V6.004c0-.563.12-1.113.347-1.616.227-.514.55-.969.969-1.34.419-.382.91-.67 1.436-.837.538-.18 1.1-.24 1.65-.18a4.147 4.147 0 0 1 2.597 1.4 4.133 4.133 0 0 1 1.004 2.776v2.01c0 .909.144 1.818.443 2.691l.371 1.113h-9.63v.001z"/></svg>';
    break;
  case 'compass':
    svgContent = '<svg width="16" height="16" viewBox="0 0 16 16" xmlns="http://www.w3.org/2000/svg"><path fill="#cccccc" d="M10.762 9.676l-3.472-1.402-1.402-3.472 3.472 1.402 1.402 3.472zM8 0a8 8 0 1 1 0 16A8 8 0 0 1 8 0zm0 1a7 7 0 1 0 0 14A7 7 0 0 0 8 1z"/></svg>';
    break;
  case 'gitsocial':
    svgContent = '<svg width="16" height="16" viewBox="0 0 200 200" xmlns="http://www.w3.org/2000/svg"><path d="m 191,100 c 0,3 -0.1,5 -0.3,8 C 187,148 158,181 118,189 75,198 33,175 16,135 -1,95 13,49 49,25 85,0 133,5 164,35 M 109,10 C 92,9 67,17 55,34 37,59 45,98 85,100 h 26 l 79,0" fill="none" stroke="#cccccc" stroke-width="18" stroke-linecap="square" stroke-linejoin="round" /></svg>';
    break;
  }

  const dataUri = `data:image/svg+xml;base64,${Buffer.from(svgContent).toString('base64')}`;
  return vscode.Uri.parse(dataUri);
};

export class WebviewManager {
  private extensionUri: vscode.Uri;
  private activePanels: Map<string, vscode.WebviewPanel> = new Map();
  private panelData: Map<string, unknown> = new Map();
  private sidebarProvider?: { refresh(): void; _handleGetLists(): Promise<void>; postMessage(message: unknown): void };

  constructor(extensionUri: vscode.Uri) {
    this.extensionUri = extensionUri;

    // Initialize broadcast callbacks for handlers
    this.initializeHandlerCallbacks();
  }

  /**
   * Set the sidebar provider reference
   */
  setSidebarProvider(
    provider: { refresh(): void; _handleGetLists(): Promise<void>; postMessage(message: unknown): void }
  ): void {
    this.sidebarProvider = provider;
  }

  /**
   * Open a webview panel
   */
  openPanel(
    viewType: string,
    title: string,
    params?: Record<string, unknown>
  ): vscode.WebviewPanel {
    // Generate panel ID based on type and params
    const panelId = this.generatePanelId(viewType, params);

    // Check if panel already exists
    const existingPanel = this.activePanels.get(panelId);
    if (existingPanel) {
      // Update the title in case it's different (e.g., repository name variations)
      existingPanel.title = title;
      // Refresh the icon in case it was lost
      this.setPanelIcon(existingPanel, viewType, params);
      // Send params update to the panel so it can react (e.g., switch tabs)
      if (params) {
        this.postMessage(existingPanel, 'updateViewParams', params);
      }
      existingPanel.reveal();
      return existingPanel;
    }

    // Create new panel
    const panel = vscode.window.createWebviewPanel(
      `gitsocial.${viewType}`,
      title,
      vscode.ViewColumn.One,
      {
        enableScripts: true,
        retainContextWhenHidden: true,
        localResourceRoots: [
          this.extensionUri,
          vscode.Uri.file(path.join(this.extensionUri.fsPath, 'out'))
        ]
      }
    );

    // Set HTML content
    panel.webview.html = this.getWebviewContent(panel.webview, viewType, params);

    // Set panel icon based on view type
    this.setPanelIcon(panel, viewType, params);

    // Store panel
    this.activePanels.set(panelId, panel);

    // Send setActivePanel message to sidebar
    this.postMessageToSidebar('setActivePanel', panelId);

    // Store post data if this is a post view
    if (viewType === 'viewPost' && params?.post) {
      this.panelData.set(panelId, params.post);
    }

    // Handle disposal
    panel.onDidDispose(() => {
      this.activePanels.delete(panelId);
      this.panelData.delete(panelId);

      // Clear active panel in sidebar if this was the active one
      this.postMessageToSidebar('setActivePanel', '');
    });

    // Handle panel visibility changes
    panel.onDidChangeViewState(() => {
      if (panel.visible) {
        this.postMessageToSidebar('setActivePanel', panelId);
      }
    });

    // Handle messages
    panel.webview.onDidReceiveMessage((message: WebviewMessage) => {
      void this.handleMessage(panel, message);
    });

    return panel;
  }

  /**
   * Post message to a specific panel with request ID support
   */
  postMessage(panel: vscode.WebviewPanel, type: string, data?: unknown, requestId?: string): void {
    const message = {
      type,
      data,
      requestId: requestId || ''
    };
    void panel.webview.postMessage(message);
  }

  /**
   * Post message to all panels and sidebar
   */
  postMessageToAll(message: ExtensionMessage): void {
    // Send to all active panels
    this.activePanels.forEach(panel => {
      void panel.webview.postMessage(message);
    });

    // Also send to sidebar if it exists
    if (this.sidebarProvider) {
      this.sidebarProvider.postMessage(message);
    }
  }

  /**
   * Get a panel by ID
   */
  getPanel(panelId: string): vscode.WebviewPanel | undefined {
    return this.activePanels.get(panelId);
  }

  /**
   * Handle messages from webviews
   */
  private async handleMessage(panel: vscode.WebviewPanel, message: WebviewMessage): Promise<void> {
    // Handle special cases that are managed directly by WebviewManager
    switch (message.type) {
    case 'ready':
      // Panel is ready, send initial data
      this.sendInitialData(panel);
      return;

    case 'openView': {
      // Open another view - use block scope to narrow type
      const openViewMessage = message ;
      log('debug', '[WebviewManager] openView message:', openViewMessage.viewType, openViewMessage.params);

      const viewType = openViewMessage.viewType;
      let params = openViewMessage.params;

      // Normalize repository URL in params to ensure consistent panel deduplication
      if (viewType === 'repository' && params?.repository) {
        params = {
          ...params,
          repository: gitMsgUrl.normalize(String(params.repository))
        };
      }

      // Proactively initialize external repositories before opening view
      // External repository initialization removed - now handled on-demand by handlers
      // when the user navigates to specific date ranges

      this.openPanel(
        viewType,
        openViewMessage.title,
        params
      );
      return;
    }

    case 'updatePanelIcon': {
      const updateMessage = message as { type: 'updatePanelIcon'; postAuthor: { email: string; repository: string } };
      const { email, repository } = updateMessage.postAuthor;
      getAvatar('user', `${email}|${repository || 'myrepository'}`, { size: 16 }).then(dataUri => {
        panel.iconPath = vscode.Uri.parse(dataUri);
      }).catch(() => {
        panel.iconPath = createPanelIcon('repo');
      });
      return;
    }
    case 'updatePanelTitle': {
      const updateMessage = message as { type: 'updatePanelTitle'; title: string };
      panel.title = updateMessage.title;
      return;
    }
    case 'closePanel':
      panel.dispose();
      return;
    }

    // Try to find a handler for this message type
    const handler = getHandler(message.type);
    if (handler) {
      await handler(panel, message);
    } else {
      console.warn('Unhandled message type:', message.type);
    }
  }

  /**
   * Generate unique panel ID
   */
  private generatePanelId(viewType: string, params?: Record<string, unknown>): string {
    if (viewType === 'viewPost' && params?.postId) {
      // Include repository in panel ID to ensure uniqueness across repos
      // Normalize repository URL to ensure consistent panel IDs
      const normalizedRepo = params.repository ? gitMsgUrl.normalize(String(params.repository)) : '';
      const repoSegment = normalizedRepo ? `-${normalizedRepo.replace(/[^a-zA-Z0-9]/g, '_')}` : '';
      return `${viewType}${repoSegment}-${String(params.postId)}`;
    }
    if (viewType === 'viewList' && params?.listId) {
      return `${viewType}-${String(params.listId)}`;
    }
    if (viewType === 'repository' && params?.repository) {
      // Normalize repository URL to ensure consistent panel IDs
      // This ensures the same repository opened from different sources reuses the same panel
      const normalizedRepo = gitMsgUrl.normalize(String(params.repository));
      return `${viewType}-${normalizedRepo.replace(/[^a-zA-Z0-9]/g, '_')}`;
    }
    return viewType;
  }

  /**
   * Get webview HTML content
   */
  private getWebviewContent(
    webview: vscode.Webview,
    viewType: string,
    params?: Record<string, unknown>
  ): string {
    const scriptUri = webview.asWebviewUri(
      vscode.Uri.joinPath(this.extensionUri, 'webview.js')
    );

    const styleUri = webview.asWebviewUri(
      vscode.Uri.joinPath(this.extensionUri, 'webview.css')
    );

    const codiconUri = webview.asWebviewUri(
      vscode.Uri.joinPath(this.extensionUri, 'codicon.css')
    );

    const nonce = this.getNonce();

    return `<!DOCTYPE html>
      <html lang="en">
      <head>
        <meta charset="UTF-8">
        <meta http-equiv="Content-Security-Policy"
              content="default-src 'none'; style-src ${webview.cspSource} 'unsafe-inline';
                       script-src 'nonce-${nonce}'; font-src ${webview.cspSource} data:;
                       img-src ${webview.cspSource} https: data:;">
        <meta name="viewport" content="width=device-width, initial-scale=1.0">
        <link href="${String(codiconUri)}" rel="stylesheet">
        <link href="${String(styleUri)}" rel="stylesheet">
        <title>GitSocial</title>
      </head>
      <body>
        <div id="app" data-view-type="${viewType}"></div>
        <script nonce="${nonce}">
          const vscode = acquireVsCodeApi();
          window.vscode = vscode;
          window.viewType = '${viewType}';
          window.viewParams = ${JSON.stringify(params || {})};
        </script>
        <script nonce="${nonce}" src="${String(scriptUri)}"></script>
      </body>
      </html>`;
  }

  /**
   * Send initial data to panel
   */
  private sendInitialData(panel: vscode.WebviewPanel): void {
    // Find panel ID
    let panelId = '';
    this.activePanels.forEach((p, id) => {
      if (p === panel) {panelId = id;}
    });

    // Send post data if available
    const postData = this.panelData.get(panelId);
    if (postData) {
      this.postMessage(panel, 'initialPost', postData);
    }

    this.postMessage(panel, 'loading', { value: false });
  }

  /**
   * Initialize handler callbacks
   */
  private initializeHandlerCallbacks(): void {
    // Import posts handler to set up callbacks
    import('./handlers/post').then(postsModule => {
      postsModule.setBroadcast(
        (message) => this.postMessageToAll(message as ExtensionMessage)
      );
    }).catch(error => {
      console.error('Failed to initialize posts handler callbacks:', error);
    });

    // Import lists handler to set up callbacks
    import('./handlers/list').then(listsModule => {
      listsModule.setListCallbacks(
        (message) => this.postMessageToAll(message as ExtensionMessage)
      );
    }).catch(error => {
      console.error('Failed to initialize lists handler callbacks:', error);
    });

    // Import repository handler to set up callbacks
    import('./handlers/repository').then(repoModule => {
      repoModule.setRepositoryCallbacks(
        (message) => this.postMessageToAll(message as ExtensionMessage)
      );
    }).catch(error => {
      console.error('Failed to initialize repository handler callbacks:', error);
    });

    // Import interactions handler to set up callbacks
    import('./handlers/interaction').then(interactionsModule => {
      interactionsModule.setInteractionCallbacks(
        (message) => this.postMessageToAll(message as ExtensionMessage)
      );
    }).catch(error => {
      console.error('Failed to initialize interactions handler callbacks:', error);
    });

    // Import misc handler to set up callbacks
    import('./handlers/misc').then(miscModule => {
      miscModule.setMiscCallbacks(
        (message) => this.postMessageToAll(message as ExtensionMessage)
      );
    }).catch(error => {
      console.error('Failed to initialize misc handler callbacks:', error);
    });
  }

  /**
   * Set panel icon based on view type
   */
  private setPanelIcon(panel: vscode.WebviewPanel, viewType: string, params?: Record<string, unknown>): void {
    if (viewType === 'viewPost' && params?.post) {
      const post = params.post as { author?: { email?: string }; repository?: string };
      if (post.author?.email) {
        getAvatar('user', `${post.author.email}|${post.repository || 'myrepository'}`, { size: 16 }).then(dataUri => {
          panel.iconPath = vscode.Uri.parse(dataUri);
        }).catch(() => {
          panel.iconPath = createPanelIcon('repo');
        });
      }
    } else if (viewType === 'repository' || viewType === 'viewRepository') {
      if (panel.title === 'Explore') {
        panel.iconPath = createPanelIcon('compass');
      } else {
        const repositoryUrl = params?.repository as string | undefined;
        if (repositoryUrl === 'myrepository' || !repositoryUrl) {
          panel.iconPath = createPanelIcon('home');
        } else {
          // Normalize repository URL for consistent avatar retrieval
          const normalizedUrl = gitMsgUrl.normalize(repositoryUrl);
          getAvatar('repo', normalizedUrl, { size: 16 }).then(dataUri => {
            panel.iconPath = vscode.Uri.parse(dataUri);
          }).catch(() => {
            panel.iconPath = createPanelIcon('repo');
          });
        }
      }
    } else if (viewType === 'viewList') {
      if (params?.listName === 'all') {
        panel.iconPath = createPanelIcon('gitsocial');
      } else {
        panel.iconPath = createPanelIcon('list');
      }
    } else if (viewType === 'timeline') {
      panel.iconPath = createPanelIcon('gitsocial');
    } else if (viewType === 'settings') {
      panel.iconPath = createPanelIcon('settings');
    } else if (viewType === 'createPost') {
      panel.iconPath = createPanelIcon('edit');
    } else if (viewType === 'search') {
      panel.iconPath = createPanelIcon('search');
    } else if (viewType === 'notifications') {
      panel.iconPath = createPanelIcon('bell');
    }
  }

  /**
   * Post message to sidebar
   */
  private postMessageToSidebar(type: string, data: unknown): void {
    // Get sidebar webview if it exists
    if (this.sidebarProvider) {
      this.sidebarProvider.postMessage({ type, data });
    }
  }

  /**
   * Generate nonce for CSP
   */
  private getNonce(): string {
    let text = '';
    const possible = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
    for (let i = 0; i < 32; i++) {
      text += possible.charAt(Math.floor(Math.random() * possible.length));
    }
    return text;
  }

}
