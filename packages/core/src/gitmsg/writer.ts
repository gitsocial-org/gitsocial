/**
 * GitMsg protocol writer
 */

import type { GitMsgHeader, GitMsgRef } from './types';

/**
 * Create a GitMsg header line from structured data
 */
export function createGitMsgHeader(header: GitMsgHeader): string {
  // Start with ext field
  const fields = [`ext="${header.ext}"`];

  // Add extension-specific fields
  for (const [key, value] of Object.entries(header.fields)) {
    fields.push(`${key}="${value}"`);
  }

  // Add version fields at the end
  fields.push(`v="${header.v}"`);
  fields.push(`ext-v="${header.extV}"`);

  // Format: --- GitMsg: ext="..."; [extension-fields]; v="..."; ext-v="..." ---
  return `--- GitMsg: ${fields.join('; ')} ---`;
}

/**
 * Create a GitMsg reference section
 */
export function createGitMsgRef(ref: GitMsgRef): string {
  // Build header line with ext field first
  const headerFields = [`ext="${ref.ext}"`];

  // Add core fields (always required)
  headerFields.push(`author="${ref.author}"`);
  headerFields.push(`email="${ref.email}"`);
  headerFields.push(`time="${ref.time}"`);

  // Add extension-specific fields
  for (const [key, value] of Object.entries(ref.fields)) {
    headerFields.push(`${key}="${value}"`);
  }

  // Add ref field
  headerFields.push(`ref="${ref.ref}"`);

  // Add version fields
  headerFields.push(`v="${ref.v}"`);
  headerFields.push(`ext-v="${ref.extV}"`);

  const headerLine = `--- GitMsg-Ref: ${headerFields.join('; ')} ---`;

  // Add metadata if present
  if (ref.metadata) {
    return `${headerLine}\n${ref.metadata}`;
  }

  return headerLine;
}

/**
 * Format a complete GitMsg message
 */
export function formatGitMsgMessage(
  content: string,
  header: GitMsgHeader,
  references: GitMsgRef[] = []
): string {
  const parts = [content.trim()];

  // Add header
  parts.push('');
  parts.push(createGitMsgHeader(header));

  // Add references
  for (const ref of references) {
    parts.push('');
    parts.push(createGitMsgRef(ref));
  }

  return parts.join('\n');
}
