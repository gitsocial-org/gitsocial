import { describe, expect, it } from 'vitest';
import { extractImages, hasMarkdownSyntax } from './markdown';

describe('markdown utilities', () => {
  describe('hasMarkdownSyntax', () => {
    it('should return false for empty string', () => {
      expect(hasMarkdownSyntax('')).toBe(false);
    });

    it('should return false for plain text', () => {
      expect(hasMarkdownSyntax('Just plain text')).toBe(false);
    });

    it('should detect code blocks', () => {
      expect(hasMarkdownSyntax('```javascript\nconst x = 1;\n```')).toBe(true);
    });

    it('should detect display math', () => {
      expect(hasMarkdownSyntax('$$E = mc^2$$')).toBe(true);
    });

    it('should detect inline math', () => {
      expect(hasMarkdownSyntax('The formula $E = mc^2$ is famous')).toBe(true);
    });

    it('should detect headings', () => {
      expect(hasMarkdownSyntax('# Heading 1')).toBe(true);
      expect(hasMarkdownSyntax('## Heading 2')).toBe(true);
      expect(hasMarkdownSyntax('###### Heading 6')).toBe(true);
    });

    it('should detect unordered lists', () => {
      expect(hasMarkdownSyntax('- Item 1')).toBe(true);
      expect(hasMarkdownSyntax('* Item 1')).toBe(true);
      expect(hasMarkdownSyntax('+ Item 1')).toBe(true);
    });

    it('should detect ordered lists', () => {
      expect(hasMarkdownSyntax('1. First item')).toBe(true);
      expect(hasMarkdownSyntax('42. Some item')).toBe(true);
    });

    it('should detect blockquotes', () => {
      expect(hasMarkdownSyntax('> Quote text')).toBe(true);
    });

    it('should detect bold text (needs multiple features)', () => {
      expect(hasMarkdownSyntax('**bold** and `code`')).toBe(true);
    });

    it('should detect links (needs multiple features)', () => {
      expect(hasMarkdownSyntax('[link](url) and **bold**')).toBe(true);
    });

    it('should detect images (needs multiple features)', () => {
      expect(hasMarkdownSyntax('![alt](url) and `code`')).toBe(true);
    });

    it('should detect inline code (needs multiple features)', () => {
      expect(hasMarkdownSyntax('`code` and **bold**')).toBe(true);
    });

    it('should require at least 2 markdown features', () => {
      expect(hasMarkdownSyntax('**bold**')).toBe(false);
      expect(hasMarkdownSyntax('[link](url)')).toBe(false);
      expect(hasMarkdownSyntax('`code`')).toBe(false);
    });
  });

  describe('extractImages', () => {
    it('should return empty array for empty string', () => {
      expect(extractImages('')).toEqual([]);
    });

    it('should return empty array for text without images', () => {
      expect(extractImages('Just plain text')).toEqual([]);
    });

    it('should extract markdown images', () => {
      const result = extractImages('![alt text](https://example.com/image.png)');
      expect(result).toContain('https://example.com/image.png');
    });

    it('should extract multiple markdown images', () => {
      const text = '![img1](https://example.com/1.png) and ![img2](https://example.com/2.jpg)';
      const result = extractImages(text);
      expect(result).toContain('https://example.com/1.png');
      expect(result).toContain('https://example.com/2.jpg');
      expect(result).toHaveLength(2);
    });

    it('should extract direct image URLs', () => {
      const text = 'Check out https://example.com/image.jpg';
      const result = extractImages(text);
      expect(result).toContain('https://example.com/image.jpg');
    });

    it('should extract various image formats', () => {
      const formats = ['jpg', 'jpeg', 'png', 'gif', 'webp', 'svg'];
      for (const format of formats) {
        const url = `https://example.com/image.${format}`;
        const result = extractImages(url);
        expect(result).toContain(url);
      }
    });

    it('should handle image URLs with query parameters', () => {
      const url = 'https://example.com/image.png?size=large&quality=high';
      const result = extractImages(url);
      expect(result).toContain(url);
    });

    it('should deduplicate images', () => {
      const text = '![img](https://example.com/image.png) https://example.com/image.png';
      const result = extractImages(text);
      expect(result).toHaveLength(1);
      expect(result[0]).toBe('https://example.com/image.png');
    });

    it('should extract both markdown and direct URLs', () => {
      const text = '![md](https://example.com/1.png) and https://example.com/2.jpg';
      const result = extractImages(text);
      expect(result).toHaveLength(2);
      expect(result).toContain('https://example.com/1.png');
      expect(result).toContain('https://example.com/2.jpg');
    });
  });
});
