/**
 * Search functionality for GitSocial posts
 */

import type { Post, Result } from './types';
import { post } from './post';
import { log } from '../logger';

/**
 * Search namespace - Search operations
 */
export const search = {
  searchPosts
};

/**
 * Search query parser result for internal use
 */
interface ParsedQuery {
  terms: string[];
  filters: {
    author?: string[];
    repo?: string[];
    type?: Array<'post' | 'comment' | 'repost' | 'quote'>;
    after?: Date;
    before?: Date;
  };
}

/**
 * Search parameters for finding posts
 */
export interface SearchParams {
  query: string
  filters?: SearchFilters
  limit?: number
  offset?: number
}

/**
 * Search filters to narrow down results
 */
export interface SearchFilters {
  author?: string
  repository?: string
  startDate?: Date
  endDate?: Date
  interactionType?: Array<'post' | 'comment' | 'repost' | 'quote'>
  branch?: string
}

/**
 * Search result containing matched posts and metadata
 */
export interface SearchResult {
  query: string
  results: SearchPost[]
  total: number
  hasMore: boolean
  executionTime: number
}

/**
 * Enhanced post with search score
 */
export interface SearchPost extends Post {
  score?: number
}

/**
 * Search posts across all repositories
 */
async function searchPosts(
  workdir: string,
  params: SearchParams
): Promise<Result<SearchResult>> {
  const startTime = Date.now();

  try {
    log('debug', '[searchPosts] Starting search with params:', params);

    // Get all posts using getPosts with scope all
    const postsResult = await post.getPosts(workdir, 'all', {
      limit: 1000 // Get more posts for search
    });

    if (!postsResult.success || !postsResult.data) {
      return {
        success: false,
        error: postsResult.error || {
          code: 'POSTS_ERROR',
          message: 'Failed to get posts'
        }
      };
    }

    // Use posts directly
    const allPosts = postsResult.data;

    // Parse the search query
    const parsed = parseSearchQuery(params.query);

    // Merge parsed filters with explicit filters
    const filters: SearchFilters = {
      ...params.filters,
      author: params.filters?.author || parsed.filters.author?.[0],
      repository: params.filters?.repository || parsed.filters.repo?.[0],
      startDate: params.filters?.startDate || parsed.filters.after,
      endDate: params.filters?.endDate || parsed.filters.before,
      interactionType: params.filters?.interactionType || parsed.filters.type
    };

    // Filter and search posts
    let results = filterAndSearchPosts(allPosts, parsed.terms, filters);

    // Sort by relevance
    results = sortByRelevance(results);

    // Apply pagination
    const limit = params.limit || 10000;
    const offset = params.offset || 0;
    const paginatedResults = results.slice(offset, offset + limit);

    const searchResult: SearchResult = {
      query: params.query,
      results: paginatedResults,
      total: results.length,
      hasMore: results.length > offset + limit,
      executionTime: Date.now() - startTime
    };

    log('debug', '[searchPosts] Search completed:', {
      query: params.query,
      totalResults: results.length,
      returnedResults: paginatedResults.length,
      executionTime: searchResult.executionTime
    });

    return {
      success: true,
      data: searchResult
    };
  } catch (error) {
    log('error', '[searchPosts] Error:', error);
    return {
      success: false,
      error: {
        code: 'SEARCH_ERROR',
        message: error instanceof Error ? error.message : 'Search failed'
      }
    };
  }
}

/**
 * Filter and search posts based on criteria
 */
function filterAndSearchPosts(
  posts: Post[],
  searchTerms: string[],
  filters: SearchFilters
): SearchPost[] {
  const results: SearchPost[] = [];

  for (const post of posts) {
    // Skip GitSocial metadata posts
    if (post.content.startsWith('GitSocial:')) {
      continue;
    }

    // Apply filters first
    if (filters.author &&
        normalizeSearchTerm(post.author.email) !== normalizeSearchTerm(filters.author)) {
      continue;
    }

    if (filters.repository &&
        !post.repository.toLowerCase().includes(filters.repository.toLowerCase())) {
      continue;
    }

    if (filters.startDate && post.timestamp < filters.startDate) {
      continue;
    }

    if (filters.endDate && post.timestamp > filters.endDate) {
      continue;
    }

    if (filters.interactionType &&
        filters.interactionType.length > 0 &&
        !filters.interactionType.includes(post.type)) {
      continue;
    }

    if (filters.branch && post.branch !== filters.branch) {
      continue;
    }

    // If we have search terms, check if post matches
    if (searchTerms.length > 0) {
      const postText = `${post.content} ${post.cleanContent || ''} ${post.author.name} ${post.author.email}`.toLowerCase();

      // Check if ALL search terms are present (AND logic)
      const matchesAllTerms = searchTerms.every(term => {
        const normalized = normalizeSearchTerm(term);
        return postText.includes(normalized);
      });

      if (!matchesAllTerms) {
        continue;
      }
    }

    // Calculate relevance score
    const score = calculateRelevanceScore(post, searchTerms);

    results.push({
      ...post,
      score
    });
  }

  return results;
}

/**
 * Sort posts by relevance score and date
 */
function sortByRelevance(posts: SearchPost[]): SearchPost[] {
  return posts.sort((a, b) => {
    // First sort by score (higher is better)
    const scoreA = a.score || 0;
    const scoreB = b.score || 0;

    if (scoreA !== scoreB) {
      return scoreB - scoreA;
    }

    // Then by date (newer first)
    return b.timestamp.getTime() - a.timestamp.getTime();
  });
}

/**
 * Parse a search query string into structured components
 */
function parseSearchQuery(query: string): ParsedQuery {
  const parsed: ParsedQuery = {
    terms: [],
    filters: {}
  };

  // Match filter patterns
  const filterRegex = /(\w+):([^\s]+)/g;
  const terms: string[] = [];

  // Extract filters
  let lastIndex = 0;
  let match: RegExpExecArray | null;

  while ((match = filterRegex.exec(query)) !== null) {
    // Add any text before this filter as search terms
    if (match.index > lastIndex) {
      const text = query.substring(lastIndex, match.index).trim();
      if (text) {
        terms.push(...tokenize(text));
      }
    }

    const filterType = match[1]?.toLowerCase() || '';
    const filterValue = match[2] || '';

    switch (filterType) {
    case 'author':
      if (!parsed.filters.author) {parsed.filters.author = [];}
      parsed.filters.author.push(filterValue);
      break;

    case 'repo':
    case 'repository':
      if (!parsed.filters.repo) {parsed.filters.repo = [];}
      parsed.filters.repo.push(filterValue);
      break;

    case 'type': {
      if (!parsed.filters.type) {parsed.filters.type = [];}
      const validTypes = ['post', 'comment', 'repost', 'quote'] as const;
      type ValidType = typeof validTypes[number];
      const isValidType = (value: string): value is ValidType =>
        validTypes.includes(value as ValidType);
      if (isValidType(filterValue)) {
        parsed.filters.type.push(filterValue);
      }
      break;
    }

    case 'after':
      parsed.filters.after = parseDate(filterValue);
      break;

    case 'before':
      parsed.filters.before = parseDate(filterValue);
      break;

    default:
      // Unknown filter, treat as regular search term
      terms.push(`${filterType}:${filterValue}`);
    }

    lastIndex = filterRegex.lastIndex;
  }

  // Add any remaining text as search terms
  if (lastIndex < query.length) {
    const text = query.substring(lastIndex).trim();
    if (text) {
      terms.push(...tokenize(text));
    }
  }

  parsed.terms = terms;
  return parsed;
}

/**
 * Tokenize a search string into individual terms
 */
function tokenize(text: string): string[] {
  // Handle quoted strings
  const quotedRegex = /"([^"]+)"/g;
  const quoted: string[] = [];
  let match: RegExpExecArray | null;

  while ((match = quotedRegex.exec(text)) !== null) {
    if (match[1]) {
      quoted.push(match[1]);
    }
  }

  // Remove quoted strings from text
  const unquoted = text.replace(quotedRegex, ' ');

  // Split on whitespace and filter empty strings
  const terms = unquoted
    .split(/\s+/)
    .filter(term => term.length > 0);

  return [...terms, ...quoted];
}

/**
 * Parse a date string
 */
function parseDate(dateStr: string): Date | undefined {
  // Support various date formats
  const date = new Date(dateStr);
  return isNaN(date.getTime()) ? undefined : date;
}

/**
 * Normalize search terms for matching
 */
function normalizeSearchTerm(term: string): string {
  return term.toLowerCase().trim();
}

/**
 * Calculate search relevance score
 */
function calculateRelevanceScore(
  post: Post,
  searchTerms: string[]
): number {
  let score = 0;
  const content = post.content.toLowerCase();
  const cleanContent = (post.cleanContent || '').toLowerCase();

  for (const term of searchTerms) {
    const normalized = normalizeSearchTerm(term);

    // Score based on where the term appears
    if (content.includes(normalized)) {
      // Subject line (first line) matches are worth more
      const firstLine = content.split('\n')[0];
      if (firstLine && firstLine.includes(normalized)) {
        score += 10;
      } else {
        score += 5;
      }
    }

    if (cleanContent.includes(normalized)) {
      score += 3;
    }

    if (normalizeSearchTerm(post.author.name).includes(normalized)) {
      score += 8;
    }

    if (normalizeSearchTerm(post.author.email).includes(normalized)) {
      score += 8;
    }
  }

  // Boost recent posts slightly
  const ageInDays = (Date.now() - post.timestamp.getTime()) / (1000 * 60 * 60 * 24);
  if (ageInDays < 7) {
    score += 2;
  } else if (ageInDays < 30) {
    score += 1;
  }

  return score;
}
