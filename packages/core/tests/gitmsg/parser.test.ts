import { describe, expect, it } from 'vitest';
import {
  extractCleanContent,
  getPostType,
  isEmptyRepost,
  parseGitMsgHeader,
  parseGitMsgMessage,
  parseGitMsgRef,
  validateGitMsgMessage
} from '../../src/gitmsg/parser';
import type { GitMsgMessage } from '../../src/gitmsg/types';

describe('parseGitMsgHeader', () => {
  it('should parse valid GitMsg header', () => {
    const header = '--- GitMsg: ext="social"; type="post"; v="0.1.0"; ext-v="0.1.0" ---';
    const result = parseGitMsgHeader(header);

    expect(result).toEqual({
      ext: 'social',
      v: '0.1.0',
      extV: '0.1.0',
      fields: { type: 'post' }
    });
  });

  it('should parse header with multiple extension fields', () => {
    const header = '--- GitMsg: ext="social"; type="comment"; parent="abc123"; v="0.1.0"; ext-v="0.1.0" ---';
    const result = parseGitMsgHeader(header);

    expect(result?.fields).toEqual({
      type: 'comment',
      parent: 'abc123'
    });
  });

  it('should parse header with kebab-case fields', () => {
    const header = '--- GitMsg: ext="social"; in-reply-to="#commit:abc123def456"; v="0.1.0"; ext-v="0.1.0" ---';
    const result = parseGitMsgHeader(header);

    expect(result?.fields['in-reply-to']).toBe('#commit:abc123def456');
  });

  it('should return null for invalid format', () => {
    expect(parseGitMsgHeader('Not a valid header')).toBeNull();
    expect(parseGitMsgHeader('--- Invalid: format ---')).toBeNull();
    expect(parseGitMsgHeader('')).toBeNull();
  });

  it('should return null when missing required fields', () => {
    expect(parseGitMsgHeader('--- GitMsg: ext="social"; v="0.1.0" ---')).toBeNull();
    expect(parseGitMsgHeader('--- GitMsg: ext="social"; ext-v="0.1.0" ---')).toBeNull();
    expect(parseGitMsgHeader('--- GitMsg: v="0.1.0"; ext-v="0.1.0" ---')).toBeNull();
  });

  it('should handle empty field values', () => {
    const header = '--- GitMsg: ext="social"; type=""; v="0.1.0"; ext-v="0.1.0" ---';
    const result = parseGitMsgHeader(header);

    expect(result?.fields.type).toBe('');
  });

  it('should parse header with special characters in values', () => {
    const header = '--- GitMsg: ext="social"; content="Hello, World!"; v="0.1.0"; ext-v="0.1.0" ---';
    const result = parseGitMsgHeader(header);

    expect(result?.fields.content).toBe('Hello, World!');
  });
});

describe('parseGitMsgRef', () => {
  it('should parse basic reference with required fields', () => {
    const refSection = '--- GitMsg-Ref: ext="social"; author="Test User"; email="test@example.com"; time="2025-10-21T12:00:00Z"; ref="#commit:abc123def456"; v="0.1.0"; ext-v="0.1.0" ---';
    const result = parseGitMsgRef(refSection);

    expect(result).toEqual({
      ext: 'social',
      ref: '#commit:abc123def456',
      v: '0.1.0',
      extV: '0.1.0',
      author: 'Test User',
      email: 'test@example.com',
      time: '2025-10-21T12:00:00Z',
      fields: {},
      metadata: undefined
    });
  });

  it('should parse reference with core fields', () => {
    const refSection = '--- GitMsg-Ref: ext="social"; author="Test User"; email="test@example.com"; time="2025-10-21T12:00:00Z"; ref="#commit:abc123def456"; v="0.1.0"; ext-v="0.1.0" ---';
    const result = parseGitMsgRef(refSection);

    expect(result?.author).toBe('Test User');
    expect(result?.email).toBe('test@example.com');
    expect(result?.time).toBe('2025-10-21T12:00:00Z');
  });

  it('should parse reference with extension-specific fields', () => {
    const refSection = '--- GitMsg-Ref: ext="social"; author="Alice"; email="alice@example.com"; ' +
      'time="2025-10-21T12:00:00Z"; type="comment"; ref="#commit:abc123def456"; v="0.1.0"; ext-v="0.1.0" ---';
    const result = parseGitMsgRef(refSection);

    expect(result?.fields).toEqual({ type: 'comment' });
  });

  it('should parse reference with metadata', () => {
    const refSection = '--- GitMsg-Ref: ext="social"; author="Bob"; email="bob@example.com"; ' +
      'time="2025-10-21T12:00:00Z"; ref="#commit:abc123def456"; v="0.1.0"; ext-v="0.1.0" ---\n' +
      'Additional metadata\n' +
      'Multiple lines';
    const result = parseGitMsgRef(refSection);

    expect(result?.metadata).toBe('Additional metadata\nMultiple lines');
  });

  it('should parse absolute repository reference', () => {
    const refSection = '--- GitMsg-Ref: ext="social"; author="Charlie"; email="charlie@example.com"; time="2025-10-21T12:00:00Z"; ref="https://github.com/user/repo#commit:abc123def456"; v="0.1.0"; ext-v="0.1.0" ---';
    const result = parseGitMsgRef(refSection);

    expect(result?.ref).toBe('https://github.com/user/repo#commit:abc123def456');
  });

  it('should return null for invalid format', () => {
    expect(parseGitMsgRef('Not a valid ref')).toBeNull();
    expect(parseGitMsgRef('--- Invalid: format ---')).toBeNull();
    expect(parseGitMsgRef('')).toBeNull();
  });

  it('should return null when missing required fields', () => {
    expect(parseGitMsgRef('--- GitMsg-Ref: ext="social"; v="0.1.0"; ext-v="0.1.0" ---')).toBeNull();
    expect(parseGitMsgRef('--- GitMsg-Ref: ref="#commit:abc"; v="0.1.0"; ext-v="0.1.0" ---')).toBeNull();
    expect(parseGitMsgRef('--- GitMsg-Ref: ext="social"; ref="#commit:abc123def456"; v="0.1.0"; ext-v="0.1.0" ---')).toBeNull();
    expect(parseGitMsgRef('--- GitMsg-Ref: ext="social"; author="Test"; ref="#commit:abc123def456"; v="0.1.0"; ext-v="0.1.0" ---')).toBeNull();
    expect(parseGitMsgRef('--- GitMsg-Ref: ext="social"; author="Test"; email="test@example.com"; ref="#commit:abc123def456"; v="0.1.0"; ext-v="0.1.0" ---')).toBeNull();
  });
});

describe('parseGitMsgMessage', () => {
  it('should parse complete message with content and header', () => {
    const message = `This is a post

--- GitMsg: ext="social"; type="post"; v="0.1.0"; ext-v="0.1.0" ---`;

    const result = parseGitMsgMessage(message);

    expect(result).toEqual({
      content: 'This is a post',
      header: {
        ext: 'social',
        v: '0.1.0',
        extV: '0.1.0',
        fields: { type: 'post' }
      },
      references: []
    });
  });

  it('should parse message with single reference', () => {
    const message = 'Replying to a post\n' +
      '\n' +
      '--- GitMsg: ext="social"; type="comment"; v="0.1.0"; ext-v="0.1.0" ---\n' +
      '\n' +
      '--- GitMsg-Ref: ext="social"; author="Alice"; email="alice@example.com"; ' +
      'time="2025-10-21T12:00:00Z"; ref="#commit:abc123def456"; v="0.1.0"; ext-v="0.1.0" ---';

    const result = parseGitMsgMessage(message);

    expect(result?.references).toHaveLength(1);
    expect(result?.references[0]?.ref).toBe('#commit:abc123def456');
  });

  it('should parse message with multiple references', () => {
    const message = 'Quote with reference\n' +
      '\n' +
      '--- GitMsg: ext="social"; type="quote"; v="0.1.0"; ext-v="0.1.0" ---\n' +
      '\n' +
      '--- GitMsg-Ref: ext="social"; author="Bob"; email="bob@example.com"; ' +
      'time="2025-10-21T10:00:00Z"; ref="#commit:abc123def456"; v="0.1.0"; ext-v="0.1.0" ---\n' +
      '\n' +
      '--- GitMsg-Ref: ext="social"; author="Charlie"; email="charlie@example.com"; ' +
      'time="2025-10-21T11:00:00Z"; ref="#commit:def456abc123"; v="0.1.0"; ext-v="0.1.0" ---';

    const result = parseGitMsgMessage(message);

    expect(result?.references).toHaveLength(2);
    expect(result?.references[0]?.ref).toBe('#commit:abc123def456');
    expect(result?.references[1]?.ref).toBe('#commit:def456abc123');
  });

  it('should parse multiline content', () => {
    const message = `First line
Second line
Third line

--- GitMsg: ext="social"; type="post"; v="0.1.0"; ext-v="0.1.0" ---`;

    const result = parseGitMsgMessage(message);

    expect(result?.content).toBe('First line\nSecond line\nThird line');
  });

  it('should return null for messages without GitMsg header', () => {
    expect(parseGitMsgMessage('Just a regular commit message')).toBeNull();
    expect(parseGitMsgMessage('')).toBeNull();
  });

  it('should return null for invalid header', () => {
    const message = `Content

--- Invalid: format ---`;

    expect(parseGitMsgMessage(message)).toBeNull();
  });

  it('should return null when header has invalid required fields', () => {
    const message = `Content

--- GitMsg: ext="social"; v="0.1.0" ---`;

    expect(parseGitMsgMessage(message)).toBeNull();
  });

  it('should handle references with metadata', () => {
    const message = 'Quote message\n' +
      '\n' +
      '--- GitMsg: ext="social"; type="quote"; v="0.1.0"; ext-v="0.1.0" ---\n' +
      '\n' +
      '--- GitMsg-Ref: ext="social"; author="Dave"; email="dave@example.com"; ' +
      'time="2025-10-21T09:00:00Z"; ref="#commit:abc123def456"; v="0.1.0"; ext-v="0.1.0" ---\n' +
      'Original quoted content\n' +
      'Multiple lines here';

    const result = parseGitMsgMessage(message);

    expect(result?.references[0]?.metadata).toBe('Original quoted content\nMultiple lines here');
  });
});

describe('validateGitMsgMessage', () => {
  const validMessage: GitMsgMessage = {
    content: 'Test post',
    header: {
      ext: 'social',
      v: '0.1.0',
      extV: '0.1.0',
      fields: { type: 'post' }
    },
    references: []
  };

  it('should validate correct message', () => {
    expect(validateGitMsgMessage(validMessage)).toBe(true);
  });

  it('should return false for null message', () => {
    expect(validateGitMsgMessage(null)).toBe(false);
  });

  it('should reject message with missing header fields', () => {
    const invalid = {
      ...validMessage,
      header: { ...validMessage.header, ext: '' }
    };
    expect(validateGitMsgMessage(invalid)).toBe(false);
  });

  it('should reject invalid extension name format', () => {
    const invalid = {
      ...validMessage,
      header: { ...validMessage.header, ext: 'Invalid-Ext' }
    };
    expect(validateGitMsgMessage(invalid)).toBe(false);

    const invalid2 = {
      ...validMessage,
      header: { ...validMessage.header, ext: '123invalid' }
    };
    expect(validateGitMsgMessage(invalid2)).toBe(false);
  });

  it('should accept valid extension name formats', () => {
    const valid = {
      ...validMessage,
      header: { ...validMessage.header, ext: 'social' }
    };
    expect(validateGitMsgMessage(valid)).toBe(true);

    const valid2 = {
      ...validMessage,
      header: { ...validMessage.header, ext: 'my-ext_123' }
    };
    expect(validateGitMsgMessage(valid2)).toBe(true);
  });

  it('should reject invalid version formats', () => {
    const invalid = {
      ...validMessage,
      header: { ...validMessage.header, v: '1.0' }
    };
    expect(validateGitMsgMessage(invalid)).toBe(false);

    const invalid2 = {
      ...validMessage,
      header: { ...validMessage.header, extV: 'invalid' }
    };
    expect(validateGitMsgMessage(invalid2)).toBe(false);
  });

  it('should validate references with commit refs', () => {
    const withRef: GitMsgMessage = {
      ...validMessage,
      references: [{
        ext: 'social',
        ref: '#commit:abc123def456',
        v: '0.1.0',
        extV: '0.1.0',
        author: 'Test User',
        email: 'test@example.com',
        time: '2025-10-21T12:00:00Z',
        fields: {}
      }]
    };
    expect(validateGitMsgMessage(withRef)).toBe(true);
  });

  it('should validate references with branch refs', () => {
    const withRef: GitMsgMessage = {
      ...validMessage,
      references: [{
        ext: 'social',
        ref: 'https://github.com/user/repo#branch:main',
        v: '0.1.0',
        extV: '0.1.0',
        author: 'Alice',
        email: 'alice@example.com',
        time: '2025-10-21T10:00:00Z',
        fields: {}
      }]
    };
    expect(validateGitMsgMessage(withRef)).toBe(true);
  });

  it('should reject reference with invalid hash length', () => {
    const invalid: GitMsgMessage = {
      ...validMessage,
      references: [{
        ext: 'social',
        ref: '#commit:abc',
        v: '0.1.0',
        extV: '0.1.0',
        author: 'Bob',
        email: 'bob@example.com',
        time: '2025-10-21T11:00:00Z',
        fields: {}
      }]
    };
    expect(validateGitMsgMessage(invalid)).toBe(false);
  });

  it('should reject reference with invalid extension', () => {
    const invalid: GitMsgMessage = {
      ...validMessage,
      references: [{
        ext: 'Invalid-Ext',
        ref: '#commit:abc123def456',
        v: '0.1.0',
        extV: '0.1.0',
        author: 'Charlie',
        email: 'charlie@example.com',
        time: '2025-10-21T09:00:00Z',
        fields: {}
      }]
    };
    expect(validateGitMsgMessage(invalid)).toBe(false);
  });

  it('should reject reference with invalid version format', () => {
    const invalid: GitMsgMessage = {
      ...validMessage,
      references: [{
        ext: 'social',
        ref: '#commit:abc123def456',
        v: '1.0',
        extV: '0.1.0',
        author: 'Dave',
        email: 'dave@example.com',
        time: '2025-10-21T08:00:00Z',
        fields: {}
      }]
    };
    expect(validateGitMsgMessage(invalid)).toBe(false);

    const invalid2: GitMsgMessage = {
      ...validMessage,
      references: [{
        ext: 'social',
        ref: '#commit:abc123def456',
        v: '0.1.0',
        extV: 'invalid',
        author: 'Eve',
        email: 'eve@example.com',
        time: '2025-10-21T07:00:00Z',
        fields: {}
      }]
    };
    expect(validateGitMsgMessage(invalid2)).toBe(false);
  });
});

describe('extractCleanContent', () => {
  it('should remove GitMsg header from message', () => {
    const message = `This is content

--- GitMsg: ext="social"; type="post"; v="0.1.0"; ext-v="0.1.0" ---`;

    const clean = extractCleanContent(message);
    expect(clean).toBe('This is content');
  });

  it('should remove GitMsg-Ref sections', () => {
    const message = `Content here

--- GitMsg: ext="social"; type="comment"; v="0.1.0"; ext-v="0.1.0" ---

--- GitMsg-Ref: ext="social"; ref="#commit:abc123def456"; v="0.1.0"; ext-v="0.1.0" ---`;

    const clean = extractCleanContent(message);
    expect(clean).toBe('Content here');
  });

  it('should remove multiple reference sections', () => {
    const message = `Quote

--- GitMsg: ext="social"; type="quote"; v="0.1.0"; ext-v="0.1.0" ---

--- GitMsg-Ref: ext="social"; ref="#commit:abc123def456"; v="0.1.0"; ext-v="0.1.0" ---

--- GitMsg-Ref: ext="social"; ref="#commit:def456abc123"; v="0.1.0"; ext-v="0.1.0" ---`;

    const clean = extractCleanContent(message);
    expect(clean).toBe('Quote');
  });

  it('should handle plain commit messages without GitMsg', () => {
    const message = 'Regular commit message';
    expect(extractCleanContent(message)).toBe('Regular commit message');
  });

  it('should handle empty content', () => {
    const message = `
--- GitMsg: ext="social"; type="post"; v="0.1.0"; ext-v="0.1.0" ---`;

    expect(extractCleanContent(message)).toBe('');
  });
});

describe('getPostType', () => {
  it('should return "post" for undefined gitMsg', () => {
    expect(getPostType(undefined)).toBe('post');
  });

  it('should return "post" for non-social extension', () => {
    const gitMsg: GitMsgMessage = {
      content: 'Test',
      header: { ext: 'other', v: '0.1.0', extV: '0.1.0', fields: {} },
      references: []
    };
    expect(getPostType(gitMsg)).toBe('post');
  });

  it('should return type from social extension', () => {
    const post: GitMsgMessage = {
      content: 'Post',
      header: { ext: 'social', v: '0.1.0', extV: '0.1.0', fields: { type: 'post' } },
      references: []
    };
    expect(getPostType(post)).toBe('post');

    const comment: GitMsgMessage = {
      content: 'Comment',
      header: { ext: 'social', v: '0.1.0', extV: '0.1.0', fields: { type: 'comment' } },
      references: []
    };
    expect(getPostType(comment)).toBe('comment');

    const repost: GitMsgMessage = {
      content: 'Repost',
      header: { ext: 'social', v: '0.1.0', extV: '0.1.0', fields: { type: 'repost' } },
      references: []
    };
    expect(getPostType(repost)).toBe('repost');

    const quote: GitMsgMessage = {
      content: 'Quote',
      header: { ext: 'social', v: '0.1.0', extV: '0.1.0', fields: { type: 'quote' } },
      references: []
    };
    expect(getPostType(quote)).toBe('quote');
  });

  it('should default to "post" for unknown type', () => {
    const gitMsg: GitMsgMessage = {
      content: 'Test',
      header: { ext: 'social', v: '0.1.0', extV: '0.1.0', fields: { type: 'unknown' } },
      references: []
    };
    expect(getPostType(gitMsg)).toBe('post');
  });

  it('should default to "post" when type field missing', () => {
    const gitMsg: GitMsgMessage = {
      content: 'Test',
      header: { ext: 'social', v: '0.1.0', extV: '0.1.0', fields: {} },
      references: []
    };
    expect(getPostType(gitMsg)).toBe('post');
  });
});

describe('isEmptyRepost', () => {
  it('should return true for simple repost with only attribution', () => {
    const repost: GitMsgMessage = {
      content: '#commit:abc123def456',
      header: { ext: 'social', v: '0.1.0', extV: '0.1.0', fields: { type: 'repost' } },
      references: []
    };
    expect(isEmptyRepost(repost)).toBe(true);
  });

  it('should return false for repost with additional content', () => {
    const repost: GitMsgMessage = {
      content: '#commit:abc123def456\nAdditional comment',
      header: { ext: 'social', v: '0.1.0', extV: '0.1.0', fields: { type: 'repost' } },
      references: []
    };
    expect(isEmptyRepost(repost)).toBe(false);
  });

  it('should return false for non-repost types', () => {
    const post: GitMsgMessage = {
      content: '#commit:abc123def456',
      header: { ext: 'social', v: '0.1.0', extV: '0.1.0', fields: { type: 'post' } },
      references: []
    };
    expect(isEmptyRepost(post)).toBe(false);
  });

  it('should return false for repost without hash attribution', () => {
    const repost: GitMsgMessage = {
      content: 'Just a repost',
      header: { ext: 'social', v: '0.1.0', extV: '0.1.0', fields: { type: 'repost' } },
      references: []
    };
    expect(isEmptyRepost(repost)).toBe(false);
  });
});
