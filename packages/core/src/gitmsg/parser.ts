/**
 * GitMsg protocol parser
 */

import { type GitMsgHeader, type GitMsgMessage, type GitMsgRef, isMessageType } from './types';

/**
 * Parse a GitMsg header line into structured data
 */
export function parseGitMsgHeader(headerLine: string): GitMsgHeader | null {
  // Header format: --- GitMsg: ext="..."; field="..."; v="..."; ext-v="..." ---
  const match = headerLine.match(/^--- GitMsg: (.*) ---$/);
  if (!match) {
    return null;
  }

  const fields: Record<string, string> = {};
  let ext = '';
  let v = '';
  let extV = '';

  // Parse semicolon-separated key="value" pairs
  const fieldRegex = /([a-zA-Z_][a-zA-Z0-9_:-]*)="([^"]*)"/g;
  let fieldMatch;
  const headerContent = match[1] || '';

  while ((fieldMatch = fieldRegex.exec(headerContent)) !== null) {
    const [, key, value] = fieldMatch;

    if (key && value !== undefined) {
      switch (key) {
      case 'ext':
        ext = value;
        break;
      case 'v':
        v = value;
        break;
      case 'ext-v':
        extV = value;
        break;
      default:
        // Extension-specific fields (e.g., "type", "original")
        fields[key] = value;
      }
    }
  }

  // Validate required fields
  if (!ext || !v || !extV) {
    return null;
  }

  return {
    ext,
    v,
    extV,
    fields
  };
}

/**
 * Parse a GitMsg reference section
 */
export function parseGitMsgRef(refSection: string): GitMsgRef | null {
  const lines = refSection.split('\n');
  const headerLine = lines[0];

  if (!headerLine) {
    return null;
  }

  // Reference format: --- GitMsg-Ref: ext="..."; author="..."; ref="..."; v="..."; ext-v="..." ---
  const match = headerLine.match(/^--- GitMsg-Ref: (.*) ---$/);
  if (!match) {
    return null;
  }

  const fields: Record<string, string> = {};
  let ext = '';
  let ref = '';
  let v = '';
  let extV = '';
  let author: string | undefined;
  let email: string | undefined;
  let time: string | undefined;

  // Parse fields from header line
  const fieldRegex = /([a-zA-Z_][a-zA-Z0-9_:-]*)="([^"]*)"/g;
  let fieldMatch;
  const refContent = match[1] || '';

  while ((fieldMatch = fieldRegex.exec(refContent)) !== null) {
    const [, key, value] = fieldMatch;

    if (key && value !== undefined) {
      switch (key) {
      case 'ext':
        ext = value;
        break;
      case 'ref':
        ref = value;
        break;
      case 'v':
        v = value;
        break;
      case 'ext-v':
        extV = value;
        break;
      case 'author':
        author = value;
        break;
      case 'email':
        email = value;
        break;
      case 'time':
        time = value;
        break;
      default:
        // Extension-specific fields
        fields[key] = value;
      }
    }
  }

  // Validate required fields
  if (!ext || !ref || !v || !extV || !author || !email || !time) {
    return null;
  }

  // Get metadata content (everything after the header line)
  const metadata = lines.slice(1).join('\n').trim() || undefined;

  return {
    ext,
    ref,
    v,
    extV,
    author,
    email,
    time,
    fields,
    metadata
  };
}

/**
 * Parse a complete GitMsg message
 */
export function parseGitMsgMessage(message: string): GitMsgMessage | null {
  // Find the GitMsg header
  const headerMatch = message.match(/^([\s\S]*?)\n--- GitMsg: .* ---$/m);
  if (!headerMatch || !headerMatch[1]) {
    return null;
  }

  const content = headerMatch[1].trim();
  const headerStart = headerMatch.index! + headerMatch[1].length + 1;

  // Extract header line
  const headerEnd = message.indexOf('---', headerStart + 11) + 3;
  const headerLine = message.substring(headerStart, headerEnd);

  const header = parseGitMsgHeader(headerLine);
  if (!header) {
    return null;
  }

  // Parse reference sections
  const references: GitMsgRef[] = [];
  const refRegex = /--- GitMsg-Ref: .*? ---[\s\S]*?(?=--- GitMsg-Ref:|$)/g;
  const remainingMessage = message.substring(headerEnd);

  let refMatch;
  while ((refMatch = refRegex.exec(remainingMessage)) !== null) {
    const ref = parseGitMsgRef(refMatch[0].trim());
    if (ref) {
      references.push(ref);
    }
  }

  return {
    content,
    header,
    references
  };
}

/**
 * Validate a GitMsg message structure
 */
export function validateGitMsgMessage(message: GitMsgMessage | null): boolean {
  if (!message) {
    return false;
  }

  // Check header
  if (!message.header.ext || !message.header.v || !message.header.extV) {
    return false;
  }

  // Validate extension format
  if (!message.header.ext.match(/^[a-z][a-z0-9_-]*$/)) {
    return false;
  }

  // Validate version format
  const versionRegex = /^\d+\.\d+\.\d+$/;
  if (!message.header.v.match(versionRegex) ||
      !message.header.extV.match(versionRegex)) {
    return false;
  }

  // Validate references
  for (const ref of message.references) {
    if (!ref.ext.match(/^[a-z][a-z0-9_-]*$/)) {
      return false;
    }

    // Validate ref format - check for valid reference patterns
    // Commits must have exactly 12-character hashes, branches can be any valid name
    const refPattern = new RegExp(
      '^((https?:\\/\\/[^#\\s]+|[^#\\s]+)#(commit:[a-f0-9]{12}|branch:[a-zA-Z0-9/_-]+)|' +
      '#(commit:[a-f0-9]{12}|branch:[a-zA-Z0-9/_-]+))$'
    );
    if (!refPattern.test(ref.ref)) {
      return false;
    }

    if (!ref.v.match(versionRegex) || !ref.extV.match(versionRegex)) {
      return false;
    }
  }

  return true;
}

/**
 * Extract clean content from commit message (remove GitMsg headers)
 * Pure utility function for text processing
 */
export function extractCleanContent(message: string): string {
  // Remove GitMsg header
  const headerRegex = /^--- GitMsg:.*?---$/m;
  let cleanMessage = message.replace(headerRegex, '').trim();

  // Remove GitMsg-Ref sections
  const refRegex = /^--- GitMsg-Ref:.*?---[\s\S]*?(?=^--- GitMsg-Ref:|$(?!\n))/gm;
  cleanMessage = cleanMessage.replace(refRegex, '').trim();

  return cleanMessage;
}

/**
 * Determine post type from GitMsg header
 * Pure function for type detection
 */
export function getPostType(gitMsg?: GitMsgMessage): 'post' | 'comment' | 'repost' | 'quote' {
  if (!gitMsg) {return 'post';}
  if (gitMsg.header.ext !== 'social') {return 'post';}

  const socialType = gitMsg.header.fields['type'];
  switch (socialType) {
  case 'comment':
    return 'comment';
  case 'repost':
    return 'repost';
  case 'quote':
    return 'quote';
  case 'post':
  default:
    return 'post';
  }
}

/**
 * Check if a repost is empty (simple repost without additional content)
 * Pure predicate function
 */
export function isEmptyRepost(gitMsg: GitMsgMessage): boolean {
  if (!isMessageType(gitMsg.header, 'social', 'repost')) {return false;}

  // A repost is empty if it only contains the attribution line (starts with #)
  const content = gitMsg.content.trim();
  return content.startsWith('#') && content.split('\n').length === 1;
}
