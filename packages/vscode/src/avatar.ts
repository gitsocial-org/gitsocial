import * as vscode from 'vscode';
import * as path from 'path';
import * as fs from 'fs';
import {
  type AvatarCacheStats,
  type ClearAvatarCacheOptions,
  type ClearResult,
  social
} from '@gitsocial/core';
import { getGitHubToken } from './auth';

let avatarDir: string | null = null;

export async function initializeAvatarSystem(context: vscode.ExtensionContext): Promise<void> {
  avatarDir = path.join(context.globalStorageUri.fsPath, 'avatars');

  const token = await getGitHubToken();
  social.avatar.setGitHubToken(token);

  const config = vscode.workspace.getConfiguration('gitsocial');
  const enableGravatar = config.get<boolean>('enableGravatar', false);
  social.avatar.setEnableGravatar(enableGravatar);

  context.subscriptions.push(
    vscode.authentication.onDidChangeSessions(async e => {
      if (e.provider.id === 'github') {
        const newToken = await getGitHubToken();
        social.avatar.setGitHubToken(newToken);
      }
    })
  );
}

export async function getAvatar(type: 'user' | 'repo', identifier: string, options?: { size?: number; context?: string; name?: string }): Promise<string> {
  if (!avatarDir) {
    throw new Error('Avatar system not initialized');
  }

  let actualIdentifier = identifier;
  let context = options?.context;

  // Handle composite identifier for users (email|repository format)
  if (type === 'user' && identifier.includes('|')) {
    const [email, repoUrl] = identifier.split('|', 2);
    actualIdentifier = email;
    context = repoUrl;
  }

  // Always fetch 80px source, ignore size parameter for storage
  const filePath = await social.avatar.getAvatar({
    avatarDir,
    type,
    identifier: actualIdentifier,
    context,
    name: options?.name,
    size: 80  // Always use 80px source for optimal quality
  });

  return convertToDataUri(filePath);
}

export async function clearAvatarCache(options?: ClearAvatarCacheOptions): Promise<ClearResult> {
  if (!avatarDir) {
    throw new Error('Avatar system not initialized');
  }
  return social.avatar.clearAvatarCache(avatarDir, options);
}

export async function getAvatarCacheStats(): Promise<AvatarCacheStats> {
  if (!avatarDir) {
    throw new Error('Avatar system not initialized');
  }
  return social.avatar.getAvatarCacheStats(avatarDir);
}

async function convertToDataUri(filePath: string): Promise<string> {
  const buffer = await fs.promises.readFile(filePath);
  const ext = path.extname(filePath).toLowerCase();

  // For SVG files, return as-is since they're already generated with circular shapes
  if (ext === '.svg') {
    return `data:image/svg+xml;base64,${buffer.toString('base64')}`;
  }

  // For image files (PNG/JPG), create a rounded version using SVG
  const imageDataUri = `data:image/${ext === '.jpg' || ext === '.jpeg' ? 'jpeg' : 'png'};base64,${buffer.toString('base64')}`;

  const roundedSvg = `
    <svg width="80" height="80" viewBox="0 0 80 80" xmlns="http://www.w3.org/2000/svg">
      <defs>
        <clipPath id="circle">
          <circle cx="40" cy="40" r="40"/>
        </clipPath>
      </defs>
      <image href="${imageDataUri}" width="80" height="80" clip-path="url(#circle)"/>
    </svg>
  `;

  return `data:image/svg+xml;base64,${Buffer.from(roundedSvg).toString('base64')}`;
}
