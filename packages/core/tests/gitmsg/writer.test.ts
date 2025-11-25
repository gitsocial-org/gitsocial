import { describe, expect, it } from 'vitest';
import { createGitMsgHeader, createGitMsgRef, formatGitMsgMessage } from '../../src/gitmsg/writer';
import { parseGitMsgHeader, parseGitMsgMessage, parseGitMsgRef } from '../../src/gitmsg/parser';
import type { GitMsgHeader, GitMsgRef } from '../../src/gitmsg/types';

describe('createGitMsgHeader', () => {
  it('should create basic header', () => {
    const header: GitMsgHeader = {
      ext: 'social',
      v: '0.1.0',
      extV: '0.1.0',
      fields: {}
    };

    const result = createGitMsgHeader(header);
    expect(result).toBe('--- GitMsg: ext="social"; v="0.1.0"; ext-v="0.1.0" ---');
  });

  it('should create header with extension fields', () => {
    const header: GitMsgHeader = {
      ext: 'social',
      v: '0.1.0',
      extV: '0.1.0',
      fields: { type: 'post' }
    };

    const result = createGitMsgHeader(header);
    expect(result).toBe('--- GitMsg: ext="social"; type="post"; v="0.1.0"; ext-v="0.1.0" ---');
  });

  it('should create header with multiple extension fields', () => {
    const header: GitMsgHeader = {
      ext: 'social',
      v: '0.1.0',
      extV: '0.1.0',
      fields: {
        type: 'comment',
        'in-reply-to': '#commit:abc123def456'
      }
    };

    const result = createGitMsgHeader(header);
    expect(result).toContain('ext="social"');
    expect(result).toContain('type="comment"');
    expect(result).toContain('in-reply-to="#commit:abc123def456"');
    expect(result).toContain('v="0.1.0"');
    expect(result).toContain('ext-v="0.1.0"');
  });

  it('should handle special characters in field values', () => {
    const header: GitMsgHeader = {
      ext: 'social',
      v: '0.1.0',
      extV: '0.1.0',
      fields: { content: 'Hello, World!' }
    };

    const result = createGitMsgHeader(header);
    expect(result).toContain('content="Hello, World!"');
  });

  it('should maintain field order (ext first, versions last)', () => {
    const header: GitMsgHeader = {
      ext: 'social',
      v: '0.1.0',
      extV: '0.1.0',
      fields: { type: 'post', priority: 'high' }
    };

    const result = createGitMsgHeader(header);
    expect(result).toMatch(/^--- GitMsg: ext="social";.*; v="0\.1\.0"; ext-v="0\.1\.0" ---$/);
  });
});

describe('createGitMsgRef', () => {
  it('should create basic reference', () => {
    const ref: GitMsgRef = {
      ext: 'social',
      ref: '#commit:abc123def456',
      v: '0.1.0',
      extV: '0.1.0',
      author: 'Test User',
      email: 'test@example.com',
      time: '2025-10-21T12:00:00Z',
      fields: {}
    };

    const result = createGitMsgRef(ref);
    expect(result).toContain('ext="social"');
    expect(result).toContain('author="Test User"');
    expect(result).toContain('email="test@example.com"');
    expect(result).toContain('time="2025-10-21T12:00:00Z"');
    expect(result).toContain('ref="#commit:abc123def456"');
  });

  it('should create reference with core fields', () => {
    const ref: GitMsgRef = {
      ext: 'social',
      ref: '#commit:abc123def456',
      v: '0.1.0',
      extV: '0.1.0',
      author: 'Test User',
      email: 'test@example.com',
      time: '2025-10-21T12:00:00Z',
      fields: {}
    };

    const result = createGitMsgRef(ref);
    expect(result).toContain('author="Test User"');
    expect(result).toContain('email="test@example.com"');
    expect(result).toContain('time="2025-10-21T12:00:00Z"');
  });

  it('should create reference with extension fields', () => {
    const ref: GitMsgRef = {
      ext: 'social',
      ref: '#commit:abc123def456',
      v: '0.1.0',
      extV: '0.1.0',
      author: 'Alice',
      email: 'alice@example.com',
      time: '2025-10-21T10:00:00Z',
      fields: { type: 'comment' }
    };

    const result = createGitMsgRef(ref);
    expect(result).toContain('type="comment"');
  });

  it('should create reference with metadata', () => {
    const ref: GitMsgRef = {
      ext: 'social',
      ref: '#commit:abc123def456',
      v: '0.1.0',
      extV: '0.1.0',
      author: 'Bob',
      email: 'bob@example.com',
      time: '2025-10-21T11:00:00Z',
      fields: {},
      metadata: 'Additional metadata\nMultiple lines'
    };

    const result = createGitMsgRef(ref);
    expect(result).toContain('--- GitMsg-Ref:');
    expect(result).toContain('Additional metadata');
    expect(result).toContain('Multiple lines');
  });

  it('should create absolute repository reference', () => {
    const ref: GitMsgRef = {
      ext: 'social',
      ref: 'https://github.com/user/repo#commit:abc123def456',
      v: '0.1.0',
      extV: '0.1.0',
      author: 'Charlie',
      email: 'charlie@example.com',
      time: '2025-10-21T09:00:00Z',
      fields: {}
    };

    const result = createGitMsgRef(ref);
    expect(result).toContain('ref="https://github.com/user/repo#commit:abc123def456"');
  });

  it('should maintain field order (ext first, ref before versions)', () => {
    const ref: GitMsgRef = {
      ext: 'social',
      ref: '#commit:abc123def456',
      v: '0.1.0',
      extV: '0.1.0',
      author: 'User',
      email: 'user@example.com',
      time: '2025-10-21T08:00:00Z',
      fields: { type: 'comment' }
    };

    const result = createGitMsgRef(ref);
    const match = result.match(/ext="[^"]+"/);
    expect(match?.index).toBeLessThan(result.indexOf('ref='));
    expect(result.indexOf('ref=')).toBeLessThan(result.indexOf('v='));
  });
});

describe('formatGitMsgMessage', () => {
  it('should format complete message with content and header', () => {
    const header: GitMsgHeader = {
      ext: 'social',
      v: '0.1.0',
      extV: '0.1.0',
      fields: { type: 'post' }
    };

    const result = formatGitMsgMessage('This is a post', header);

    expect(result).toContain('This is a post');
    expect(result).toContain('--- GitMsg: ext="social"; type="post"; v="0.1.0"; ext-v="0.1.0" ---');
  });

  it('should format message with references', () => {
    const header: GitMsgHeader = {
      ext: 'social',
      v: '0.1.0',
      extV: '0.1.0',
      fields: { type: 'comment' }
    };

    const references: GitMsgRef[] = [{
      ext: 'social',
      ref: '#commit:abc123def456',
      v: '0.1.0',
      extV: '0.1.0',
      author: 'Alice',
      email: 'alice@example.com',
      time: '2025-10-21T12:00:00Z',
      fields: {}
    }];

    const result = formatGitMsgMessage('A comment', header, references);

    expect(result).toContain('A comment');
    expect(result).toContain('--- GitMsg: ext="social"; type="comment"');
    expect(result).toContain('--- GitMsg-Ref: ext="social"; author="Alice"');
  });

  it('should format message with multiple references', () => {
    const header: GitMsgHeader = {
      ext: 'social',
      v: '0.1.0',
      extV: '0.1.0',
      fields: { type: 'quote' }
    };

    const references: GitMsgRef[] = [
      {
        ext: 'social',
        ref: '#commit:abc123def456',
        v: '0.1.0',
        extV: '0.1.0',
        author: 'Bob',
        email: 'bob@example.com',
        time: '2025-10-21T10:00:00Z',
        fields: {}
      },
      {
        ext: 'social',
        ref: '#commit:def456abc123',
        v: '0.1.0',
        extV: '0.1.0',
        author: 'Charlie',
        email: 'charlie@example.com',
        time: '2025-10-21T11:00:00Z',
        fields: {}
      }
    ];

    const result = formatGitMsgMessage('A quote', header, references);

    expect(result.match(/--- GitMsg-Ref:/g)).toHaveLength(2);
  });

  it('should trim content and add proper spacing', () => {
    const header: GitMsgHeader = {
      ext: 'social',
      v: '0.1.0',
      extV: '0.1.0',
      fields: { type: 'post' }
    };

    const result = formatGitMsgMessage('  Content with spaces  ', header);

    expect(result).toMatch(/^Content with spaces\n\n--- GitMsg:/);
  });

  it('should handle multiline content', () => {
    const header: GitMsgHeader = {
      ext: 'social',
      v: '0.1.0',
      extV: '0.1.0',
      fields: { type: 'post' }
    };

    const content = 'Line 1\nLine 2\nLine 3';
    const result = formatGitMsgMessage(content, header);

    expect(result).toContain('Line 1\nLine 2\nLine 3');
  });

  it('should separate sections with blank lines', () => {
    const header: GitMsgHeader = {
      ext: 'social',
      v: '0.1.0',
      extV: '0.1.0',
      fields: { type: 'comment' }
    };

    const references: GitMsgRef[] = [{
      ext: 'social',
      ref: '#commit:abc123def456',
      v: '0.1.0',
      extV: '0.1.0',
      author: 'Dave',
      email: 'dave@example.com',
      time: '2025-10-21T09:00:00Z',
      fields: {}
    }];

    const result = formatGitMsgMessage('Content', header, references);

    const lines = result.split('\n');
    expect(lines[1]).toBe(''); // Blank line after content
    expect(lines[3]).toBe(''); // Blank line before reference
  });
});

describe('round-trip parsing and formatting', () => {
  it('should parse and format simple message identically', () => {
    const original = `This is a post

--- GitMsg: ext="social"; type="post"; v="0.1.0"; ext-v="0.1.0" ---`;

    const parsed = parseGitMsgMessage(original);
    expect(parsed).not.toBeNull();

    const formatted = formatGitMsgMessage(
      parsed!.content,
      parsed!.header,
      parsed!.references
    );

    const reparsed = parseGitMsgMessage(formatted);
    expect(reparsed).toEqual(parsed);
  });

  it('should preserve header fields through round-trip', () => {
    const header: GitMsgHeader = {
      ext: 'social',
      v: '0.1.0',
      extV: '0.1.0',
      fields: {
        type: 'comment',
        'in-reply-to': '#commit:abc123def456'
      }
    };

    const formatted = createGitMsgHeader(header);
    const parsed = parseGitMsgHeader(formatted);

    expect(parsed).toEqual(header);
  });

  it('should preserve reference fields through round-trip', () => {
    const ref: GitMsgRef = {
      ext: 'social',
      ref: '#commit:abc123def456',
      v: '0.1.0',
      extV: '0.1.0',
      author: 'Test User',
      email: 'test@example.com',
      time: '2025-10-21T12:00:00Z',
      fields: { type: 'comment' }
    };

    const formatted = createGitMsgRef(ref);
    const parsed = parseGitMsgRef(formatted);

    expect(parsed).toEqual({ ...ref, metadata: undefined });
  });

  it('should preserve message with references through round-trip', () => {
    const header: GitMsgHeader = {
      ext: 'social',
      v: '0.1.0',
      extV: '0.1.0',
      fields: { type: 'comment' }
    };

    const references: GitMsgRef[] = [{
      ext: 'social',
      ref: '#commit:abc123def456',
      v: '0.1.0',
      extV: '0.1.0',
      author: 'Eve',
      email: 'eve@example.com',
      time: '2025-10-21T08:00:00Z',
      fields: {}
    }];

    const formatted = formatGitMsgMessage('Test comment', header, references);
    const parsed = parseGitMsgMessage(formatted);

    expect(parsed).not.toBeNull();
    expect(parsed!.content).toBe('Test comment');
    expect(parsed!.header).toEqual(header);
    expect(parsed!.references).toHaveLength(1);
    expect(parsed!.references[0]).toEqual({ ...references[0], metadata: undefined });
  });

  it('should preserve metadata in references through round-trip', () => {
    const ref: GitMsgRef = {
      ext: 'social',
      ref: '#commit:abc123def456',
      v: '0.1.0',
      extV: '0.1.0',
      author: 'Frank',
      email: 'frank@example.com',
      time: '2025-10-21T07:00:00Z',
      fields: {},
      metadata: 'Quoted content'
    };

    const formatted = createGitMsgRef(ref);
    const parsed = parseGitMsgRef(formatted);

    expect(parsed?.metadata).toBe('Quoted content');
  });
});
