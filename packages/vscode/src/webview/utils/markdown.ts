import { marked } from 'marked';

interface LinkToken {
  href: string;
  title?: string | null;
  text: string;
  tokens?: unknown;
}

interface ImageToken {
  href: string;
  title?: string | null;
  text?: string;
  tokens?: unknown;
}

interface CodeToken {
  lang?: string | null;
  text: string;
}

interface CodespanToken {
  text: string;
}

let isConfigured = false;

export function configureMarked(): void {
  if (isConfigured) {return;}

  marked.setOptions({
    breaks: true,  // GitHub-style line breaks
    gfm: true     // GitHub Flavored Markdown
  });

  // Custom renderer for security
  const renderer = new marked.Renderer();

  // Safe link rendering - ensure external links open securely
  renderer.link = function(token: LinkToken): string {
    const href = token.href || '';
    const title = token.title ? ` title="${escapeHtml(token.title)}"` : '';
    const text = token.text || '';
    return `<a href="${escapeHtml(href)}"${title}>${escapeHtml(text)}</a>`;
  };

  // Safe image rendering
  renderer.image = function(token: ImageToken): string {
    const href = token.href || '';
    const title = token.title ? ` title="${escapeHtml(token.title)}"` : '';
    const text = token.text ? ` alt="${escapeHtml(token.text)}"` : '';
    return `<img src="${escapeHtml(href)}" loading="lazy"${title}${text} style="max-width: 100%; height: auto;" />`;
  };

  // Code block with simple styling
  renderer.code = function(token: CodeToken): string {
    const lang = token.lang ? ` class="language-${escapeHtml(token.lang)}"` : '';
    return `<pre><code${lang}>${escapeHtml(token.text)}</code></pre>`;
  };

  // Inline code
  renderer.codespan = function(token: CodespanToken): string {
    return `<code>${escapeHtml(token.text)}</code>`;
  };

  marked.use({ renderer });
  isConfigured = true;
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

const emojiMap: Record<string, string> = {
  'art': '🎨',
  'zap': '⚡️',
  'fire': '🔥',
  'bug': '🐛',
  'ambulance': '🚑️',
  'sparkles': '✨',
  'memo': '📝',
  'rocket': '🚀',
  'lipstick': '💄',
  'tada': '🎉',
  'white_check_mark': '✅',
  'lock': '🔒️',
  'closed_lock_with_key': '🔐',
  'bookmark': '🔖',
  'rotating_light': '🚨',
  'construction': '🚧',
  'green_heart': '💚',
  'arrow_down': '⬇️',
  'arrow_up': '⬆️',
  'pushpin': '📌',
  'construction_worker': '👷',
  'chart_with_upwards_trend': '📈',
  'recycle': '♻️',
  'heavy_plus_sign': '➕',
  'heavy_minus_sign': '➖',
  'wrench': '🔧',
  'hammer': '🔨',
  'globe_with_meridians': '🌐',
  'pencil2': '✏️',
  'poop': '💩',
  'rewind': '⏪️',
  'twisted_rightwards_arrows': '🔀',
  'package': '📦️',
  'alien': '👽️',
  'truck': '🚚',
  'page_facing_up': '📄',
  'boom': '💥',
  'bento': '🍱',
  'wheelchair': '♿️',
  'bulb': '💡',
  'beers': '🍻',
  'speech_balloon': '💬',
  'card_file_box': '🗃️',
  'loud_sound': '🔊',
  'mute': '🔇',
  'busts_in_silhouette': '👥',
  'children_crossing': '🚸',
  'building_construction': '🏗️',
  'iphone': '📱',
  'clown_face': '🤡',
  'egg': '🥚',
  'see_no_evil': '🙈',
  'camera_flash': '📸',
  'alembic': '⚗️',
  'mag': '🔍️',
  'label': '🏷️',
  'seedling': '🌱',
  'triangular_flag_on_post': '🚩',
  'goal_net': '🥅',
  'dizzy': '💫',
  'wastebasket': '🗑️',
  'passport_control': '🛂',
  'adhesive_bandage': '🩹',
  'monocle_face': '🧐',
  'coffin': '⚰️',
  'test_tube': '🧪',
  'necktie': '👔',
  'stethoscope': '🩺',
  'bricks': '🧱',
  'technologist': '🧑‍💻',
  'money_with_wings': '💸',
  'thread': '🧵',
  'safety_vest': '🦺',
  'airplane': '✈️'
};

function processEmojis(text: string): string {
  return text.replace(/:([a-zA-Z_]+):/g, (match, emojiName: string) => {
    const emoji = emojiMap[emojiName.toLowerCase()];
    return emoji || match;
  });
}

export function parseMarkdown(content: string): {
  html: string;
  text: string;
  firstLine: string;
  remainingContent: string;
} {
  if (!content) {
    return { html: '', text: '', firstLine: '', remainingContent: '' };
  }

  // Ensure marked is configured
  configureMarked();

  try {
    // Process emojis first
    const processedContent = processEmojis(content);

    const lines = processedContent.split('\n');
    const firstLine = lines[0].trim();
    const remainingContent = lines.slice(1).join('\n');

    // Check if first line is a markdown block element
    const isHeading = /^#{1,6}\s/.test(firstLine);
    const isUnorderedList = /^[-*+]\s/.test(firstLine);
    const isOrderedList = /^\d+\.\s/.test(firstLine);
    const isCodeBlock = /^```/.test(firstLine);
    const isBlockquote = /^>\s/.test(firstLine);
    const isHorizontalRule = /^(---+|\*\*\*+|___+)$/.test(firstLine);
    const isBlockElement = isHeading || isUnorderedList || isOrderedList ||
      isCodeBlock || isBlockquote || isHorizontalRule;

    if (isBlockElement) {
      // For block elements, parse the entire content normally (no manual first line bolding)
      const html = marked.parse(processedContent) as string;
      return { html, text: content, firstLine: lines[0], remainingContent };
    } else {
      // For regular text, split and make first line bold
      let html = '';

      if (firstLine) {
        const firstLineHtml = marked.parseInline(firstLine) as string;
        html += `<div class="font-bold">${firstLineHtml}</div>`;
      }

      if (remainingContent.trim()) {
        const remainingHtml = marked.parse(remainingContent) as string;
        html += remainingHtml;
      }

      return { html, text: content, firstLine: lines[0], remainingContent };
    }
  } catch (error) {
    console.warn('Markdown parsing error:', error);
    // Fallback to escaped plain text with emojis processed
    const processedContent = processEmojis(content);
    return {
      html: `<div class="whitespace-pre-wrap">${escapeHtml(processedContent)}</div>`,
      text: content,
      firstLine: content.split('\n')[0],
      remainingContent: content.split('\n').slice(1).join('\n')
    };
  }
}

export function extractImages(content: string): string[] {
  if (!content) {return [];}

  const urls = new Set<string>();

  // Markdown images: ![alt](url)
  const mdImages = content.matchAll(/!\[([^\]]*)\]\(([^)]+)\)/g);
  for (const match of mdImages) {
    urls.add(match[2]);
  }

  // Direct image URLs
  const directUrls = content.matchAll(/https?:\/\/\S+\.(?:jpg|jpeg|gif|png|webp|svg)(?:\?\S*)?/gi);
  for (const match of directUrls) {
    urls.add(match[0]);
  }

  return Array.from(urls);
}

export function stripMarkdown(content: string): string {
  if (!content) {return '';}

  // Process emojis first before stripping markdown
  const processedContent = processEmojis(content);

  // Simple markdown stripping (can be enhanced later)
  return processedContent
    .replace(/!\[([^\]]*)\]\([^)]+\)/g, '') // Remove images
    .replace(/\[([^\]]+)\]\([^)]+\)/g, '$1') // Remove links, keep text
    .replace(/[*_~`#]/g, '') // Remove formatting characters
    .replace(/^[-*+]\s+/gm, '') // Remove list markers
    .replace(/^\d+\.\s+/gm, '') // Remove numbered list markers
    .replace(/^>\s+/gm, '') // Remove blockquote markers
    .trim();
}
