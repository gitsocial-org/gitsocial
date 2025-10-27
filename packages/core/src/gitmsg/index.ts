/**
 * GitMsg protocol module exports
 */

import * as parser from './parser';
import * as writer from './writer';
import * as types from './types';

// Export namespaces - MAIN EXPORTS
export const gitMsg = {
  // Parser functions
  parseMessage: parser.parseGitMsgMessage,
  parseHeader: parser.parseGitMsgHeader,
  parseRef: parser.parseGitMsgRef,
  validateMessage: parser.validateGitMsgMessage,

  // Writer functions
  createHeader: writer.createGitMsgHeader,
  createRef: writer.createGitMsgRef,
  formatMessage: writer.formatGitMsgMessage,

  // Type validation functions
  isMessageType: types.isMessageType
};

export { gitMsgList } from './lists';

// Export types explicitly
export type { GitMsgHeader, GitMsgRef, GitMsgMessage } from './types';

// Export validation functions explicitly
export { isMessageType } from './types';
