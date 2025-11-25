import { describe, expect, it } from 'vitest';
import { gitMsg, type GitMsgHeader, gitMsgList, type GitMsgMessage, type GitMsgRef, isMessageType } from '../../src/gitmsg/index';

describe('gitmsg/index', () => {
  describe('gitMsg namespace', () => {
    it('should export parseMessage', () => {
      expect(gitMsg.parseMessage).toBeDefined();
      expect(typeof gitMsg.parseMessage).toBe('function');
    });

    it('should export parseHeader', () => {
      expect(gitMsg.parseHeader).toBeDefined();
      expect(typeof gitMsg.parseHeader).toBe('function');
    });

    it('should export parseRef', () => {
      expect(gitMsg.parseRef).toBeDefined();
      expect(typeof gitMsg.parseRef).toBe('function');
    });

    it('should export validateMessage', () => {
      expect(gitMsg.validateMessage).toBeDefined();
      expect(typeof gitMsg.validateMessage).toBe('function');
    });

    it('should export createHeader', () => {
      expect(gitMsg.createHeader).toBeDefined();
      expect(typeof gitMsg.createHeader).toBe('function');
    });

    it('should export createRef', () => {
      expect(gitMsg.createRef).toBeDefined();
      expect(typeof gitMsg.createRef).toBe('function');
    });

    it('should export formatMessage', () => {
      expect(gitMsg.formatMessage).toBeDefined();
      expect(typeof gitMsg.formatMessage).toBe('function');
    });

    it('should export isMessageType', () => {
      expect(gitMsg.isMessageType).toBeDefined();
      expect(typeof gitMsg.isMessageType).toBe('function');
    });

    it('should call gitMsg.isMessageType', () => {
      const header: GitMsgHeader = {
        ext: 'social',
        v: '0.1.0',
        extV: '0.1.0',
        fields: { type: 'post' }
      };
      const result = gitMsg.isMessageType(header, 'social', 'post');
      expect(result).toBe(true);
    });

    it('should call gitMsg.parseHeader', () => {
      const result = gitMsg.parseHeader('--- GitMsg: ext="social"; type="post"; v="0.1.0"; ext-v="0.1.0" ---');
      expect(result).not.toBeNull();
      expect(result?.ext).toBe('social');
      expect(result?.fields.type).toBe('post');
    });

    it('should call gitMsg.createHeader', () => {
      const header: GitMsgHeader = {
        ext: 'social',
        v: '0.1.0',
        extV: '0.1.0',
        fields: { type: 'post' }
      };
      const result = gitMsg.createHeader(header);
      expect(result).toContain('ext="social"');
      expect(result).toContain('type="post"');
    });

    it('should call gitMsg.parseRef', () => {
      const result = gitMsg.parseRef('--- GitMsg-Ref: ext="social"; author="Test"; email="test@example.com"; time="2025-10-21T12:00:00Z"; ref="#commit:abc123456789"; v="0.1.0"; ext-v="0.1.0" ---');
      expect(result).not.toBeNull();
      expect(result?.ref).toBe('#commit:abc123456789');
    });

    it('should call gitMsg.createRef', () => {
      const ref: GitMsgRef = {
        ext: 'social',
        ref: '#commit:abc123456789',
        v: '0.1.0',
        extV: '0.1.0',
        author: 'Test',
        email: 'test@example.com',
        time: '2025-10-21T12:00:00Z',
        fields: {}
      };
      const result = gitMsg.createRef(ref);
      expect(result).toContain('#commit:abc123456789');
      expect(result).toContain('ext="social"');
    });

    it('should call gitMsg.formatMessage', () => {
      const header: GitMsgHeader = {
        ext: 'social',
        v: '0.1.0',
        extV: '0.1.0',
        fields: { type: 'post' }
      };
      const message = gitMsg.formatMessage('Hello', header);
      expect(message).toContain('ext="social"');
      expect(message).toContain('Hello');
    });

    it('should call gitMsg.parseMessage', () => {
      const input = 'Hello\n\n--- GitMsg: ext="social"; type="post"; v="0.1.0"; ext-v="0.1.0" ---';
      const result = gitMsg.parseMessage(input);
      expect(result).not.toBeNull();
      expect(result?.header.ext).toBe('social');
    });

    it('should call gitMsg.validateMessage', () => {
      const message: GitMsgMessage = {
        header: {
          ext: 'social',
          v: '0.1.0',
          extV: '0.1.0',
          fields: { type: 'post' }
        },
        content: 'Hello',
        references: []
      };
      const result = gitMsg.validateMessage(message);
      expect(result).toBe(true);
    });
  });

  describe('gitMsgList export', () => {
    it('should export gitMsgList namespace', () => {
      expect(gitMsgList).toBeDefined();
      expect(typeof gitMsgList.read).toBe('function');
      expect(typeof gitMsgList.write).toBe('function');
      expect(typeof gitMsgList.delete).toBe('function');
      expect(typeof gitMsgList.enumerate).toBe('function');
      expect(typeof gitMsgList.getHistory).toBe('function');
    });
  });

  describe('standalone exports', () => {
    it('should export isMessageType function', () => {
      const header: GitMsgHeader = {
        ext: 'social',
        v: '0.1.0',
        extV: '0.1.0',
        fields: { type: 'post' }
      };
      expect(isMessageType).toBeDefined();
      expect(isMessageType(header, 'social', 'post')).toBe(true);
      expect(isMessageType(header, 'social', 'comment')).toBe(false);
      expect(isMessageType(header, 'other', 'post')).toBe(false);
    });

    it('should export GitMsgHeader type', () => {
      const header: GitMsgHeader = {
        ext: 'social',
        v: '0.1.0',
        extV: '0.1.0',
        fields: { type: 'post' }
      };
      expect(header.ext).toBe('social');
      expect(header.v).toBe('0.1.0');
    });

    it('should export GitMsgRef type', () => {
      const ref: GitMsgRef = {
        ext: 'social',
        ref: '#commit:abc123456789',
        v: '0.1.0',
        extV: '0.1.0',
        author: 'Test',
        email: 'test@example.com',
        time: '2025-10-21T12:00:00Z',
        fields: {}
      };
      expect(ref.ext).toBe('social');
      expect(ref.ref).toBe('#commit:abc123456789');
    });

    it('should export GitMsgMessage type', () => {
      const message: GitMsgMessage = {
        header: {
          ext: 'social',
          v: '0.1.0',
          extV: '0.1.0',
          fields: { type: 'post' }
        },
        content: 'Hello world',
        references: []
      };
      expect(message.header.ext).toBe('social');
      expect(message.content).toBe('Hello world');
    });
  });
});
