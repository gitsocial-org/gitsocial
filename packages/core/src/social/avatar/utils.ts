import * as crypto from 'crypto';

function md5Hash(input: string): string {
  return crypto.createHash('md5').update(input.toLowerCase().trim()).digest('hex');
}

function extractInitialsFromName(name: string): string {
  if (!name) {return '?';}

  const parts = name.trim().split(/\s+/).filter(Boolean);
  if (parts.length === 0) {return '?';}
  if (parts.length === 1 && parts[0]) {return parts[0].charAt(0).toUpperCase();}

  // First name + Last name initials
  const first = parts[0];
  const last = parts[parts.length - 1];
  if (first && last) {
    return (first.charAt(0) + last.charAt(0)).toUpperCase();
  }

  return '?';
}

function extractGitHubUsername(identifier: string): string {
  // Extract from GitHub noreply email
  const match = identifier.match(/\d+\+(.+)@users\.noreply\.github\.com/);
  if (match && match[1]) {
    return match[1].charAt(0).toUpperCase();
  }

  // Extract from regular email
  if (identifier.includes('@')) {
    const username = identifier.split('@')[0];
    if (username) {
      return username.charAt(0).toUpperCase();
    }
  }

  // Extract from repository URL
  if (identifier.includes('/')) {
    const parts = identifier.split('/');
    const repoName = parts[parts.length - 1] || identifier;
    return repoName.charAt(0).toUpperCase();
  }

  // Default: first character
  return identifier.charAt(0).toUpperCase() || '?';
}

// Removed createRoundedAvatarSvg - now saving PNG files directly
// This eliminates double base64 encoding and reduces file sizes by ~70%

function createLetterAvatarSvg(identifier: string, size: number, name?: string): string {
  const letter = name ? extractInitialsFromName(name) : extractGitHubUsername(identifier);
  const color = md5Hash(identifier).substring(0, 6);
  const fontSize = letter.length > 1 ? size * 0.33 : size * 0.4;

  return `<svg xmlns="http://www.w3.org/2000/svg" width="${size}" height="${size}" viewBox="0 0 ${size} ${size}">
    <circle cx="${size / 2}" cy="${size / 2}" r="${size / 2}" fill="#${color}"/>
    <text x="${size / 2}" y="${size / 2}" font-family="Arial" font-size="${fontSize}" font-weight="bold"
          fill="white" text-anchor="middle" dominant-baseline="central">${letter}</text>
  </svg>`;
}

function createHomeIconAvatarSvg(size: number): string {
  const color = '#666666'; // Neutral gray
  const iconSize = size * 0.6;
  const iconOffset = (size - iconSize) / 2;

  return `<svg xmlns="http://www.w3.org/2000/svg" width="${size}" height="${size}" viewBox="0 0 ${size} ${size}">
    <circle cx="${size / 2}" cy="${size / 2}" r="${size / 2}" fill="${color}"/>
    <g transform="translate(${iconOffset}, ${iconOffset}) scale(${iconSize / 16})">
      <path fill="white" fill-rule="evenodd" clip-rule="evenodd"
            d="M8.36 1.37l6.36 5.8-.71.71L13 6.964v6.526l-.5.5h-3l-.5-.5v-3.5H7v3.5l-.5.5h-3l-.5-.5V6.972L2 7.88
            l-.71-.71 6.35-5.8h.72zM4 6.063v6.927h2v-3.5l.5-.5h3l.5.5v3.5h2V6.057L8 2.43 4 6.063z"/>
    </g>
  </svg>`;
}

/**
 * Avatar utils namespace - Avatar utility functions
 */
export const avatarUtils = {
  md5Hash,
  extractGitHubUsername,
  createLetterAvatarSvg,
  createHomeIconAvatarSvg
};
