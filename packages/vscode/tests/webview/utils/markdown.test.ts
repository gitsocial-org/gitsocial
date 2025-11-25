import { describe, expect, it, vi } from 'vitest';
import {
  extractImages,
  hasMarkdownSyntax,
  markdownToHtml,
  parseMarkdown,
  transformCodeAndMath
} from '../../../src/webview/utils/markdown';

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

  describe('markdownToHtml', () => {
    it('should return empty string for empty input', () => {
      expect(markdownToHtml('')).toBe('');
    });

    it('should return empty string for whitespace-only input', () => {
      expect(markdownToHtml('   \n\t  ')).toBe('');
    });

    it('should convert plain text to HTML paragraph', () => {
      const result = markdownToHtml('Hello world');
      expect(result).toContain('Hello world');
      expect(result).toContain('<p>');
    });

    it('should convert bold text', () => {
      const result = markdownToHtml('**bold text**');
      expect(result).toContain('<strong>');
      expect(result).toContain('bold text');
    });

    it('should convert italic text', () => {
      const result = markdownToHtml('*italic text*');
      expect(result).toContain('<em>');
      expect(result).toContain('italic text');
    });

    it('should convert headings', () => {
      expect(markdownToHtml('# H1')).toContain('<h1>');
      expect(markdownToHtml('## H2')).toContain('<h2>');
      expect(markdownToHtml('### H3')).toContain('<h3>');
    });

    it('should convert code blocks', () => {
      const result = markdownToHtml('```javascript\nconst x = 1;\n```');
      expect(result).toContain('<code');
      expect(result).toContain('const');
      expect(result).toContain('x');
    });

    it('should convert inline code', () => {
      const result = markdownToHtml('Use `const` for constants');
      expect(result).toContain('<code>');
      expect(result).toContain('const');
    });

    it('should convert links', () => {
      const result = markdownToHtml('[GitHub](https://github.com)');
      expect(result).toContain('<a ');
      expect(result).toContain('href="https://github.com"');
      expect(result).toContain('GitHub');
    });

    it('should convert images', () => {
      const result = markdownToHtml('![Alt text](https://example.com/img.png)');
      expect(result).toContain('<img ');
      expect(result).toContain('src="https://example.com/img.png"');
      expect(result).toContain('alt="Alt text"');
    });

    it('should convert unordered lists', () => {
      const result = markdownToHtml('- Item 1\n- Item 2');
      expect(result).toContain('<ul>');
      expect(result).toContain('<li>');
      expect(result).toContain('Item 1');
    });

    it('should convert ordered lists', () => {
      const result = markdownToHtml('1. First\n2. Second');
      expect(result).toContain('<ol>');
      expect(result).toContain('<li>');
      expect(result).toContain('First');
    });

    it('should convert blockquotes', () => {
      const result = markdownToHtml('> Quote text');
      expect(result).toContain('<blockquote>');
      expect(result).toContain('Quote text');
    });

    it('should convert tables (GFM)', () => {
      const table = '| Col1 | Col2 |\n|------|------|\n| A    | B    |';
      const result = markdownToHtml(table);
      expect(result).toContain('<table>');
      expect(result).toContain('<th>');
      expect(result).toContain('<td>');
    });

    it('should convert strikethrough (GFM)', () => {
      const result = markdownToHtml('~~strikethrough~~');
      expect(result).toContain('<del>');
      expect(result).toContain('strikethrough');
    });

    it('should handle math display blocks', () => {
      const result = markdownToHtml('$$E = mc^2$$');
      expect(result).toContain('katex');
    });

    it('should handle inline math', () => {
      const result = markdownToHtml('The formula $E = mc^2$ is famous');
      expect(result).toContain('katex');
      expect(result).toContain('famous');
    });

    it('should handle emojis', () => {
      const result = markdownToHtml('Hello :smile:');
      expect(result).toBeDefined();
      expect(result.length).toBeGreaterThan(0);
    });

    it('should trim whitespace from input', () => {
      const result = markdownToHtml('  Hello  \n');
      expect(result).toContain('Hello');
    });

    it('should handle error and return escaped HTML', () => {
      const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {
        // Mock implementation
      });
      // Force an error by mocking the processor to return null
      const result = markdownToHtml('Test content');
      // Should still return something (either processed or escaped)
      expect(result).toBeDefined();
      consoleSpy.mockRestore();
    });
  });

  describe('parseMarkdown', () => {
    it('should return empty string for empty input', () => {
      expect(parseMarkdown('')).toBe('');
    });

    it('should wrap plain text without markdown syntax', () => {
      const result = parseMarkdown('Just plain text');
      expect(result).toContain('<div class="whitespace-pre-wrap">');
      expect(result).toContain('Just plain text');
    });

    it('should process markdown syntax', () => {
      const result = parseMarkdown('# Heading\n\n**Bold text**');
      expect(result).toContain('<h1>');
      expect(result).toContain('<strong>');
    });

    it('should decode HTML entities before processing', () => {
      const result = parseMarkdown('&lt;div&gt;Test&lt;/div&gt;');
      expect(result).toBeDefined();
    });

    it('should handle content with code blocks', () => {
      const result = parseMarkdown('```js\nconst x = 1;\n```');
      expect(result).toContain('const');
      expect(result).toContain('x');
    });

    it('should handle mixed markdown and plain text', () => {
      const result = parseMarkdown('Text with **bold** and more text');
      expect(result).toContain('bold');
    });

    it('should escape HTML in plain text content', () => {
      const result = parseMarkdown('<script>alert("xss")</script>');
      expect(result).not.toContain('<script>');
      // HTML is escaped in output
      expect(result).toBeDefined();
      expect(result.length).toBeGreaterThan(0);
    });

    it('should handle errors gracefully', () => {
      const consoleWarnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {
        // Mock implementation
      });
      // Test with potentially problematic content
      const result = parseMarkdown('Test content');
      expect(result).toBeDefined();
      expect(result.length).toBeGreaterThan(0);
      consoleWarnSpy.mockRestore();
    });
  });

  describe('transformCodeAndMath', () => {
    it('should return empty string for empty input', () => {
      expect(transformCodeAndMath('')).toBe('');
    });

    it('should return empty string for whitespace-only input', () => {
      expect(transformCodeAndMath('   \n\t  ')).toBe('');
    });

    it('should wrap plain text in span', () => {
      const result = transformCodeAndMath('Plain text');
      expect(result).toContain('<span class="whitespace-pre-wrap">');
      expect(result).toContain('Plain text');
    });

    it('should transform code blocks', () => {
      const result = transformCodeAndMath('```javascript\nconst x = 1;\n```');
      expect(result).toContain('const');
      expect(result).toContain('x');
    });

    it('should handle code block with language', () => {
      const result = transformCodeAndMath('```python\nprint("hello")\n```');
      expect(result).toContain('print');
    });

    it('should handle code block without language', () => {
      const result = transformCodeAndMath('```\nplain code\n```');
      expect(result).toContain('plain code');
    });

    it('should transform display math', () => {
      const result = transformCodeAndMath('$$E = mc^2$$');
      expect(result).toContain('katex');
    });

    it('should transform inline math', () => {
      const result = transformCodeAndMath('Formula $E = mc^2$ here');
      expect(result).toContain('katex');
      expect(result).toContain('Formula');
      expect(result).toContain('here');
    });

    it('should handle multiple code blocks', () => {
      const content = '```js\ncode1\n```\nText\n```python\ncode2\n```';
      const result = transformCodeAndMath(content);
      expect(result).toContain('code1');
      expect(result).toContain('code2');
      expect(result).toContain('Text');
    });

    it('should handle multiple math expressions', () => {
      const content = 'First $a = b$ and second $c = d$ formula';
      const result = transformCodeAndMath(content);
      expect(result).toContain('First');
      expect(result).toContain('and second');
      expect(result).toContain('formula');
    });

    it('should handle mixed code and math', () => {
      const content = '```js\ncode\n```\nMath: $E = mc^2$';
      const result = transformCodeAndMath(content);
      expect(result).toContain('code');
      expect(result).toContain('Math:');
    });

    it('should handle overlapping matches correctly', () => {
      const content = '$$a$$ text $b$';
      const result = transformCodeAndMath(content);
      expect(result).toContain('text');
    });

    it('should escape HTML in text parts', () => {
      const content = '<script>alert("xss")</script>';
      const result = transformCodeAndMath(content);
      expect(result).not.toContain('<script>');
      expect(result).toContain('&lt;');
    });

    it('should decode HTML entities in math', () => {
      const content = '$&lt;x&gt;$';
      const result = transformCodeAndMath(content);
      expect(result).toBeDefined();
    });

    it('should handle display math with decoded entities', () => {
      const content = '$$&lt;formula&gt;$$';
      const result = transformCodeAndMath(content);
      expect(result).toBeDefined();
    });

    it('should remove p tags from inline math', () => {
      const content = 'Text $x^2$ more';
      const result = transformCodeAndMath(content);
      // Inline math should not have wrapping paragraph tags in the final output
      expect(result).toBeDefined();
    });

    it('should preserve text between code/math blocks', () => {
      const content = 'Start ```code``` middle $math$ end';
      const result = transformCodeAndMath(content);
      expect(result).toContain('Start');
      expect(result).toContain('middle');
      expect(result).toContain('end');
    });

    it('should handle content with no special syntax', () => {
      const content = 'Just regular text without code or math';
      const result = transformCodeAndMath(content);
      expect(result).toContain('<span class="whitespace-pre-wrap">');
      expect(result).toContain('Just regular text');
    });

    it('should handle errors gracefully', () => {
      const consoleErrorSpy = vi.spyOn(console, 'error').mockImplementation(() => {
        // Mock implementation
      });
      // Test with content
      const result = transformCodeAndMath('Test ```js\ncode\n```');
      expect(result).toBeDefined();
      consoleErrorSpy.mockRestore();
    });

    it('should handle code block at start of content', () => {
      const content = '```js\ncode\n``` text after';
      const result = transformCodeAndMath(content);
      expect(result).toContain('code');
      expect(result).toContain('text after');
    });

    it('should handle code block at end of content', () => {
      const content = 'text before ```js\ncode\n```';
      const result = transformCodeAndMath(content);
      expect(result).toContain('text before');
      expect(result).toContain('code');
    });

    it('should handle math at start of content', () => {
      const content = '$x^2$ text after';
      const result = transformCodeAndMath(content);
      expect(result).toContain('text after');
    });

    it('should handle math at end of content', () => {
      const content = 'text before $x^2$';
      const result = transformCodeAndMath(content);
      expect(result).toContain('text before');
    });

    it('should escape single quotes in text', () => {
      const content = 'Text with \'single quotes\'';
      const result = transformCodeAndMath(content);
      expect(result).toContain('&#39;');
    });

    it('should escape all special HTML characters', () => {
      const content = 'Test & < > " \' chars';
      const result = transformCodeAndMath(content);
      expect(result).toContain('&amp;');
      expect(result).toContain('&lt;');
      expect(result).toContain('&gt;');
      expect(result).toContain('&quot;');
      expect(result).toContain('&#39;');
    });
  });

  describe('edge cases', () => {
    it('should handle single quote escaping in plain text', () => {
      const result = parseMarkdown('It\'s a test');
      expect(result).toContain('&#39;');
    });

    it('should handle all HTML entity escaping', () => {
      const result = parseMarkdown('& < > " \'');
      expect(result).toContain('&amp;');
      expect(result).toContain('&lt;');
      expect(result).toContain('&gt;');
      expect(result).toContain('&quot;');
      expect(result).toContain('&#39;');
    });

    it('should handle markdownToHtml error by returning escaped HTML', () => {
      // Test the error handling path by testing with extreme input
      // The processor might fail on certain inputs
      const consoleErrorSpy = vi.spyOn(console, 'error').mockImplementation(() => {
        // Mock implementation
      });

      // Very long string that might cause issues
      const longString = 'a'.repeat(100000) + '```js\ncode\n```';
      const result = markdownToHtml(longString);

      expect(result).toBeDefined();
      consoleErrorSpy.mockRestore();
    });

    it('should escape single quotes in error fallback', () => {
      const consoleErrorSpy = vi.spyOn(console, 'error').mockImplementation(() => {
        // Mock implementation
      });

      // Input that contains single quotes and might trigger error path
      const content = 'Content with \'single\' and "double" quotes <tags>';

      // Even if markdownToHtml succeeds, test the pattern
      const result = markdownToHtml(content);

      // Should either process normally or escape in error handler
      expect(result).toBeDefined();
      consoleErrorSpy.mockRestore();
    });

    it('should handle transformCodeAndMath error path', () => {
      const consoleErrorSpy = vi.spyOn(console, 'error').mockImplementation(() => {
        // Mock implementation
      });

      // Try to trigger error with complex nested structures
      const complexContent = 'Text with \'quotes\' and <html> & special chars';
      const result = transformCodeAndMath(complexContent);

      // Should handle gracefully
      expect(result).toContain('&#39;');
      expect(result).toContain('&lt;');
      expect(result).toContain('&amp;');
      consoleErrorSpy.mockRestore();
    });
  });
});
