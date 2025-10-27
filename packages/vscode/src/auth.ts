import * as vscode from 'vscode';
import { log } from '@gitsocial/core';

export async function getGitHubToken(): Promise<string | null> {
  try {
    const session = await vscode.authentication.getSession('github', ['user:email'], {
      createIfNone: false,
      silent: true
    });

    if (session) {
      log('info', '[Auth] GitHub token detected - using authenticated rate limit (5,000 req/hr)');
      return session.accessToken;
    }
  } catch (error) {
    // Silent failure - user not authenticated or auth provider unavailable
  }

  log('info', '[Auth] No GitHub token - using unauthenticated rate limit (60 req/hr)');
  return null;
}
