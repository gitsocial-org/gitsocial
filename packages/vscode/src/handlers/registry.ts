import type * as vscode from 'vscode';
import type { BaseWebviewMessage } from './types';

export type MessageHandler<T extends BaseWebviewMessage = BaseWebviewMessage> = (
  panel: vscode.WebviewPanel,
  message: T
) => Promise<void> | void;

const handlers = new Map<string, MessageHandler>();

export function registerHandler<T extends BaseWebviewMessage>(
  type: T['type'],
  handler: MessageHandler<T>
): void {
  handlers.set(type, handler as MessageHandler);
}

export function getHandler(type: string): MessageHandler | undefined {
  return handlers.get(type);
}
