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

export function parseMarkdown(content: string): {
	html: string
	text: string
	firstLine: string
	remainingContent: string
} {
  if (!content) {
    return { html: '', text: '', firstLine: '', remainingContent: '' };
  }
  try {
    const lines = content.split('\n');
    const firstLine = lines[0].trim();
    const remainingContent = lines.slice(1).join('\n');
    const isHeading = /^#{1,6}\s/.test(firstLine);
    const isUnorderedList = /^[-*+]\s/.test(firstLine);
    const isOrderedList = /^\d+\.\s/.test(firstLine);
    const isCodeBlock = /^```/.test(firstLine);
    const isBlockquote = /^>\s/.test(firstLine);
    const isHorizontalRule = /^(---+|\*\*\*+|___+)$/.test(firstLine);
    const isBlockElement = isHeading || isUnorderedList || isOrderedList
      || isCodeBlock || isBlockquote || isHorizontalRule;
    if (isBlockElement) {
      const html = markdownToHtml(content);
      return { html, text: content, firstLine: lines[0], remainingContent };
    } else {
      let html = '';
      if (firstLine) {
        const firstLineHtml = markdownToHtml(firstLine);
        html += `<div class="font-bold">${firstLineHtml}</div>`;
      }
      if (remainingContent.trim()) {
        const remainingHtml = markdownToHtml(remainingContent);
        html += remainingHtml;
      }
      return { html, text: content, firstLine: lines[0], remainingContent };
    }
  } catch (error) {
    console.warn('Markdown parsing error:', error);
    return {
      html: `<div class="whitespace-pre-wrap">${escapeHtml(content)}</div>`,
      text: content,
      firstLine: content.split('\n')[0],
      remainingContent: content.split('\n').slice(1).join('\n')
    };
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
