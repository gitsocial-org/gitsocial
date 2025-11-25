import * as vscode from 'vscode';

export async function waitForWebviewPanel(
  label: string | RegExp,
  timeout = 5000
): Promise<boolean> {
  const startTime = Date.now();
  while (Date.now() - startTime < timeout) {
    const tabGroups = vscode.window.tabGroups.all;
    const webviewTab = tabGroups
      .flatMap(group => group.tabs)
      .find(tab => {
        const labelMatch = typeof label === 'string'
          ? tab.label === label
          : label.test(tab.label);
        return labelMatch && tab.input instanceof vscode.TabInputWebview;
      });
    if (webviewTab) {
      return true;
    }
    await new Promise(resolve => setTimeout(resolve, 50));
  }
  throw new Error(`Webview panel "${label}" not found within ${timeout}ms`);
}

export async function waitForCommand(
  commandId: string,
  timeout = 5000
): Promise<void> {
  const startTime = Date.now();
  while (Date.now() - startTime < timeout) {
    const commands = await vscode.commands.getCommands();
    if (commands.includes(commandId)) {return;}
    await new Promise(resolve => setTimeout(resolve, 50));
  }
  throw new Error(`Command "${commandId}" not registered within ${timeout}ms`);
}

export async function waitForCondition(
  condition: () => boolean | Promise<boolean>,
  timeout = 5000
): Promise<void> {
  const startTime = Date.now();
  while (Date.now() - startTime < timeout) {
    const result = await condition();
    if (result) {return;}
    await new Promise(resolve => setTimeout(resolve, 50));
  }
  throw new Error(`Condition not met within ${timeout}ms`);
}
