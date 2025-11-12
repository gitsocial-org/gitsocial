import { describe, expect, it } from 'vitest';
import { type GitMsgHeader, isMessageType } from './types';

describe('gitmsg/types', () => {
  describe('isMessageType()', () => {
    it('should return true when ext and type match', () => {
      const header: GitMsgHeader = {
        ext: 'social',
        v: '0.1.0',
        extV: '0.1.0',
        fields: { type: 'post' }
      };
      expect(isMessageType(header, 'social', 'post')).toBe(true);
    });

    it('should return false when ext matches but type does not', () => {
      const header: GitMsgHeader = {
        ext: 'social',
        v: '0.1.0',
        extV: '0.1.0',
        fields: { type: 'post' }
      };
      expect(isMessageType(header, 'social', 'comment')).toBe(false);
    });

    it('should return false when type matches but ext does not', () => {
      const header: GitMsgHeader = {
        ext: 'social',
        v: '0.1.0',
        extV: '0.1.0',
        fields: { type: 'post' }
      };
      expect(isMessageType(header, 'other', 'post')).toBe(false);
    });

    it('should return false when neither ext nor type match', () => {
      const header: GitMsgHeader = {
        ext: 'social',
        v: '0.1.0',
        extV: '0.1.0',
        fields: { type: 'post' }
      };
      expect(isMessageType(header, 'other', 'comment')).toBe(false);
    });

    it('should return false when header is undefined', () => {
      expect(isMessageType(undefined, 'social', 'post')).toBe(false);
    });

    it('should return false when fields does not contain type key', () => {
      const header: GitMsgHeader = {
        ext: 'social',
        v: '0.1.0',
        extV: '0.1.0',
        fields: { other: 'value' }
      };
      expect(isMessageType(header, 'social', 'post')).toBe(false);
    });

    it('should return false when fields is empty object', () => {
      const header: GitMsgHeader = {
        ext: 'social',
        v: '0.1.0',
        extV: '0.1.0',
        fields: {}
      };
      expect(isMessageType(header, 'social', 'post')).toBe(false);
    });

    it('should handle different extension names', () => {
      const header: GitMsgHeader = {
        ext: 'custom',
        v: '0.1.0',
        extV: '0.1.0',
        fields: { type: 'message' }
      };
      expect(isMessageType(header, 'custom', 'message')).toBe(true);
      expect(isMessageType(header, 'social', 'message')).toBe(false);
    });

    it('should be case-sensitive for ext and type', () => {
      const header: GitMsgHeader = {
        ext: 'social',
        v: '0.1.0',
        extV: '0.1.0',
        fields: { type: 'post' }
      };
      expect(isMessageType(header, 'Social', 'post')).toBe(false);
      expect(isMessageType(header, 'social', 'Post')).toBe(false);
    });
  });
});
