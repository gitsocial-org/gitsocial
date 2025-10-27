import type { Post } from '@gitsocial/core/client';

export type SortType = 'top' | 'latest' | 'oldest';

function parseTimestamp(timestamp: Date | string): number {
  if (timestamp instanceof Date) {
    return timestamp.getTime();
  }
  const parsed = new Date(timestamp);
  if (isNaN(parsed.getTime())) {
    console.warn('[sortPosts] Invalid timestamp:', timestamp);
    return 0;
  }
  return parsed.getTime();
}

export function sortPosts(posts: Post[], sortBy: SortType): Post[] {
  return [...posts].sort((a, b) => {
    switch (sortBy) {
    case 'oldest':
      return parseTimestamp(a.timestamp) - parseTimestamp(b.timestamp);

    case 'top': {
      const aCount = a.interactions?.comments || 0;
      const bCount = b.interactions?.comments || 0;
      if (aCount !== bCount) {
        return bCount - aCount; // Higher comment count first
      }
      // Fallback to latest for ties
      return parseTimestamp(b.timestamp) - parseTimestamp(a.timestamp);
    }

    case 'latest':
    default:
      return parseTimestamp(b.timestamp) - parseTimestamp(a.timestamp);
    }
  });
}
