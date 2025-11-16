import { type Processor, unified } from 'unified';
import remarkParse from 'remark-parse';
import remarkGfm from 'remark-gfm';
import remarkMath from 'remark-math';
import remarkEmoji from 'remark-emoji';
import remarkRehype from 'remark-rehype';
import rehypeKatex from 'rehype-katex';
import rehypePrism from 'rehype-prism-plus';
import rehypeStringify from 'rehype-stringify';

// eslint-disable-next-line @typescript-eslint/no-explicit-any
let processor: Processor<any, any, any, any, any> | null = null;

// eslint-disable-next-line @typescript-eslint/no-explicit-any
function getProcessor(): Processor<any, any, any, any, any> {
  if (!processor) {
    processor = unified()
      .use(remarkParse)
      .use(remarkGfm)
      .use(remarkMath)
      .use(remarkEmoji)
      .use(remarkRehype)
      .use(rehypeKatex)
      .use(rehypePrism, { ignoreMissing: true })
      .use(rehypeStringify);
  }
  return processor;
}

export function markdownToHtml(markdown: string): string {
  if (!markdown || !markdown.trim()) {
    return '';
  }
  try {
    const proc = getProcessor();
    if (!proc) {
      throw new Error('Failed to initialize processor');
    }
    const result = proc.processSync(markdown.trim());
    return String(result);
  } catch (error) {
    console.error('[Milkdown] Failed to convert markdown:', error);
    return `<div class="whitespace-pre-wrap">${escapeHtml(markdown)}</div>`;
  }
}

function escapeHtml(text: string): string {
  const map: Record<string, string> = {
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    '"': '&quot;',
    '\'': '&#39;'
  };
  return text.replace(/[&<>"']/g, m => map[m]);
}

export function hasMarkdownSyntax(content: string): boolean {
  if (!content) {
    return false;
  }
  const firstLine = content.split('\n')[0];
  if (/```/.test(content) || /\$\$/.test(content) || /\$(?=\S)((?:[^$\n]|\\\$)+?)(?<=\S)\$(?!\d)/.test(content)) {
    return true;
  }
  if (/^#{1,6}\s/.test(firstLine) ||
      /^[-*+]\s/.test(firstLine) ||
      /^\d+\.\s/.test(firstLine) ||
      /^>\s/.test(firstLine)) {
    return true;
  }
  let count = 0;
  if (/\*\*.+?\*\*/.test(content)) {count++;}
  if (/\[.+?\]\(.+?\)/.test(content)) {count++;}
  if (/!\[.+?\]\(.+?\)/.test(content)) {count++;}
  if (/`[^`\n]+`/.test(content)) {count++;}
  return count >= 2;
}

export function parseMarkdown(content: string): string {
  if (!content) {
    return '';
  }
  content = decodeHtmlEntities(content);
  if (!hasMarkdownSyntax(content)) {
    return `<div class="whitespace-pre-wrap">${escapeHtml(content)}</div>`;
  }
  try {
    return markdownToHtml(content);
  } catch (error) {
    console.warn('Markdown parsing error:', error);
    return `<div class="whitespace-pre-wrap">${escapeHtml(content)}</div>`;
  }
}

function decodeHtmlEntities(text: string): string {
  const textarea = document.createElement('textarea');
  textarea.innerHTML = text;
  return textarea.value;
}

export function transformCodeAndMath(content: string): string {
  if (!content || !content.trim()) {
    return '';
  }
  try {
    const parts: Array<{ type: 'text' | 'code' | 'math', content: string, language?: string, isDisplay?: boolean }> = [];
    const codeBlockRegex = /```([a-zA-Z0-9_+#-]*)\n([\s\S]*?)```/g;
    const mathDisplayRegex = /\$\$((?:[^$]|\$(?!\$))+?)\$\$/g;
    // eslint-disable-next-line no-useless-escape
    const mathInlineRegex = /\$(?=\S)((?:[^$\n]|\\\$)+?)(?<=\S)\$(?!\d)/g;
    const allMatches: Array<{ index: number, length: number, type: 'code' | 'math', content: string, language?: string, isDisplay?: boolean }> = [];
    for (const match of content.matchAll(codeBlockRegex)) {
      allMatches.push({
        index: match.index,
        length: match[0].length,
        type: 'code',
        content: match[2],
        language: match[1] || 'text'
      });
    }
    for (const match of content.matchAll(mathDisplayRegex)) {
      allMatches.push({
        index: match.index,
        length: match[0].length,
        type: 'math',
        content: match[1],
        isDisplay: true
      });
    }
    for (const match of content.matchAll(mathInlineRegex)) {
      allMatches.push({
        index: match.index,
        length: match[0].length,
        type: 'math',
        content: match[1],
        isDisplay: false
      });
    }
    allMatches.sort((a, b) => a.index - b.index);
    let lastIndex = 0;
    for (const match of allMatches) {
      if (match.index < lastIndex) {
        continue;
      }
      if (match.index > lastIndex) {
        parts.push({
          type: 'text',
          content: content.slice(lastIndex, match.index)
        });
      }
      parts.push({
        type: match.type,
        content: match.content,
        language: match.language,
        isDisplay: match.isDisplay
      });
      lastIndex = match.index + match.length;
    }
    if (lastIndex < content.length) {
      parts.push({
        type: 'text',
        content: content.slice(lastIndex)
      });
    }
    if (parts.length === 0) {
      return `<span class="whitespace-pre-wrap">${escapeHtml(content)}</span>`;
    }
    let html = '';
    for (const part of parts) {
      if (part.type === 'text') {
        html += `<span class="whitespace-pre-wrap">${escapeHtml(part.content)}</span>`;
      } else if (part.type === 'code') {
        const codeMarkdown = `\`\`\`${part.language || ''}\n${part.content}\n\`\`\``;
        const processedHtml = markdownToHtml(codeMarkdown);
        html += processedHtml;
      } else if (part.type === 'math') {
        const decodedContent = decodeHtmlEntities(part.content);
        const mathMarkdown = part.isDisplay ? `$$${decodedContent}$$` : `$${decodedContent}$`;
        let processedHtml = markdownToHtml(mathMarkdown);
        if (!part.isDisplay) {
          processedHtml = processedHtml.replace(/^<p>(.*)<\/p>$/s, '$1');
        }
        html += processedHtml;
      }
    }
    return html;
  } catch (error) {
    console.error('Failed to transform code and math:', error);
    return `<span class="whitespace-pre-wrap">${escapeHtml(content)}</span>`;
  }
}

export function extractImages(content: string): string[] {
  if (!content) {
    return [];
  }
  const urls = new Set<string>();
  const mdImages = content.matchAll(/!\[([^\]]*)\]\(([^)]+)\)/g);
  for (const match of mdImages) {
    urls.add(match[2]);
  }
  const directUrls = content.matchAll(/https?:\/\/\S+\.(?:jpg|jpeg|gif|png|webp|svg)(?:\?\S*)?/gi);
  for (const match of directUrls) {
    urls.add(match[0]);
  }
  return Array.from(urls);
}
