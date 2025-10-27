/**
 * GitMsg protocol types
 */

/** GitMsg header fields */
export interface GitMsgHeader {
  ext: string;               // Extension name (e.g., "social")
  v: string;                 // Protocol version (e.g., "0.1.0")
  extV: string;              // Extension version (e.g., "0.1.0")
  fields: Record<string, string>; // Extension-specific fields (e.g., "type": "post")
}

/** GitMsg reference section */
export interface GitMsgRef {
  ext: string;               // Extension name (e.g., "social")
  ref: string;               // Repository URL#CommitHash
  v: string;                 // Protocol version
  extV: string;              // Extension version
  author: string;            // Core field: commit author name
  email: string;             // Core field: commit author email
  time: string;              // Core field: commit timestamp (ISO 8601)
  fields: Record<string, string>; // Extension-specific fields without core fields
  metadata?: string;         // Optional reference metadata content
}

/** Complete GitMsg message */
export interface GitMsgMessage {
  content: string;           // User content (subject + body)
  header: GitMsgHeader;      // Parsed header
  references: GitMsgRef[];   // Array of references (can be empty)
}

/** Type guard to check if a message header matches extension and type */
export function isMessageType(header: GitMsgHeader | undefined, ext: string, type: string): boolean {
  return header?.ext === ext && header?.fields['type'] === type;
}
