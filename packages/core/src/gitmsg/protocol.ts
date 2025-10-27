/**
 * GitMsg Protocol Layer - Pure protocol-compliant parsing and formatting
 * These functions follow the GitMsg specification and are browser-safe
 */

/**
 * GitMsg Reference operations - handles all reference string operations
 */
export const gitMsgRef = {
  /**
   * Create a GitMsg reference string
   */
  create(type: 'commit' | 'branch' | 'list', value: string, repository?: string): string {
    // Normalize commit hashes to 12 characters as per GitMsg spec
    const normalizedValue = type === 'commit'
      ? value.toLowerCase().substring(0, 12)
      : value;

    if (repository) {
      return `${repository}#${type}:${normalizedValue}`;
    }
    return `#${type}:${normalizedValue}`;
  },

  /**
   * Parse a GitMsg reference string into components
   */
  parse(ref: string): { type: string; repository?: string; value: string } {
    // Try to determine the type from the reference
    if (ref.includes('#commit:')) {
      const repoMatch = ref.match(/^([^#]+)#/);
      const repository = repoMatch?.[1] ? gitMsgUrl.normalize(repoMatch[1]) : undefined;
      const commitMatch = ref.match(/#commit:([^#\s]+)$/);
      const hash = commitMatch?.[1]?.toLowerCase().substring(0, 12) || '';
      return {
        type: 'commit',
        repository,
        value: hash
      };
    }

    if (ref.includes('#branch:')) {
      const repoMatch = ref.match(/^([^#]+)#/);
      const repository = repoMatch?.[1] ? gitMsgUrl.normalize(repoMatch[1]) : undefined;
      const branchMatch = ref.match(/#branch:([^#\s]+)$/);
      const branch = branchMatch?.[1] || '';
      return {
        type: 'branch',
        repository,
        value: branch
      };
    }

    if (ref.includes('#list:')) {
      const repoMatch = ref.match(/^([^#]+)#/);
      const repository = repoMatch?.[1] ? gitMsgUrl.normalize(repoMatch[1]) : undefined;
      const listMatch = ref.match(/#list:([^#\s]+)$/);
      const listId = listMatch?.[1] || '';
      return {
        type: 'list',
        repository,
        value: listId
      };
    }

    // Fallback for unparseable references
    return {
      type: 'unknown',
      value: ref
    };
  },

  /**
   * Validate a GitMsg reference format
   */
  validate(ref: string, type?: 'commit' | 'branch' | 'list'): boolean {
    if (!ref) {return false;}

    const commitPattern = /^((https?:\/\/[^#\s]+|[^#\s]+)#commit:[a-f0-9]{12}|#commit:[a-f0-9]{12})$/;
    const branchPattern = /^((https?:\/\/[^#\s]+|[^#\s]+)#branch:[a-zA-Z0-9/_-]+|#branch:[a-zA-Z0-9/_-]+)$/;
    const listPattern = /^((https?:\/\/[^#\s]+|[^#\s]+)#list:[a-zA-Z0-9_-]{1,40}|#list:[a-zA-Z0-9_-]{1,40})$/;

    if (type === 'commit') {return commitPattern.test(ref);}
    if (type === 'branch') {return branchPattern.test(ref);}
    if (type === 'list') {return listPattern.test(ref);}
    return commitPattern.test(ref) || branchPattern.test(ref) || listPattern.test(ref);
  },

  /**
   * Validate list name format according to GitMsg spec
   */
  validateListName(name: string): boolean {
    return /^[a-zA-Z0-9_-]{1,40}$/.test(name);
  },

  /**
   * Normalize a GitMsg reference (ensures 12-char hashes)
   */
  normalize(ref: string): string {
    if (!ref) {return ref;}

    const parsed = this.parse(ref);
    if (parsed.type === 'commit') {
      return this.create('commit', parsed.value, parsed.repository);
    }
    return ref;
  },

  /**
   * Check if a reference is from my repository (no repository specified)
   */
  isMyRepository(ref: string): boolean {
    return ref.startsWith('#');
  },

  /**
   * Parse a repository identifier into components
   * Handles both formats: url#branch:name and plain url (defaults to 'main')
   */
  parseRepositoryId(identifier: string): { repository: string; branch: string } {
    if (identifier.includes('#branch:')) {
      const parts = identifier.split('#branch:');
      if (parts.length === 2 && parts[0] && parts[1]) {
        return {
          repository: gitMsgUrl.normalize(parts[0]),
          branch: parts[1]
        };
      }
    }

    // Default to 'main' branch if not specified
    return {
      repository: gitMsgUrl.normalize(identifier),
      branch: 'main'
    };
  },

  /**
   * Extract branch from remote reference
   */
  extractBranchFromRemote(remoteBranch: string): string {
    if (remoteBranch.startsWith('remotes/')) {
      const parts = remoteBranch.split('/');
      if (parts.length >= 3) {
        return parts.slice(2).join('/');
      }
      return remoteBranch;
    }

    // Handle other remote patterns (e.g. upstream/main)
    if (remoteBranch.includes('/')) {
      const slashIndex = remoteBranch.indexOf('/');
      if (slashIndex > 0) {
        return remoteBranch.substring(slashIndex + 1);
      }
    }

    return remoteBranch;
  },

  /**
   * Normalize commit hash reference with repository context for references from my repository
   */
  normalizeHashInRefWithContext(ref: string, currentRepository?: string): string {
    if (!ref) {return ref;}
    const parsed = this.parse(ref);
    if (parsed.type === 'commit' && !parsed.repository && currentRepository) {
      let normalizedCurrentRepo = currentRepository;
      if (normalizedCurrentRepo && normalizedCurrentRepo.startsWith('http')) {
        normalizedCurrentRepo = normalizedCurrentRepo.replace(/\.git$/, '');
      }
      return this.create('commit', parsed.value, normalizedCurrentRepo);
    }
    return ref;
  }
};

/**
 * GitMsg URL operations - handles URL normalization and validation
 */
export const gitMsgUrl = {
  /**
   * Normalize URL to canonical form (no .git, lowercase hostname)
   */
  normalize(url: string): string {
    if (!url) {return url;}

    let normalized = url.trim();

    // Convert SSH to HTTPS format
    if (normalized.startsWith('git@')) {
      normalized = normalized.replace(/^git@([^:]+):/, 'https://$1/');
    }

    // Remove .git suffix
    normalized = normalized.replace(/\.git$/, '');

    // Lowercase hostname only
    if (normalized.includes('://')) {
      const match = normalized.match(/^(\w+:\/\/)([^/]+)(.*)$/);
      if (match && match[1] && match[2]) {
        const protocol = match[1].toLowerCase();
        const hostname = match[2].toLowerCase();
        const path = match[3] || '';
        normalized = protocol + hostname + path;
      }
    }

    return normalized;
  },

  /**
   * Validate if a string is a valid Git URL
   */
  validate(url: string): boolean {
    if (!url || typeof url !== 'string') {return false;}
    const trimmed = url.trim();

    // Accept SSH format
    if (trimmed.startsWith('git@')) {
      return /^git@[^:]+:[^/]+\/[^/]+/.test(trimmed);
    }

    // Accept HTTPS format
    if (trimmed.startsWith('https://')) {
      try {
        const urlObj = new URL(trimmed);
        return Boolean(urlObj.host) && Boolean(urlObj.pathname) && urlObj.pathname.split('/').length >= 3;
      } catch {
        return false;
      }
    }

    return false;
  },

  /**
   * Convert URL to Git-compatible format (adds .git suffix)
   */
  toGit(url: string): string {
    if (!url) {return url;}
    const trimmed = url.trim();
    if (!trimmed.endsWith('.git')) {
      return trimmed + '.git';
    }
    return trimmed;
  },

  /**
   * Extract repository URL from a GitMsg reference
   */
  fromRef(ref: string): string | null {
    const parsed = gitMsgRef.parse(ref);
    return parsed.repository || null;
  },

  /**
   * Parse URL fragment (extracts branch or other info after #)
   */
  parseFragment(url: string): { base: string; fragment?: string; branch?: string } {
    const hashIndex = url.indexOf('#');
    if (hashIndex === -1) {
      return { base: url };
    }

    const base = url.slice(0, hashIndex);
    const fragment = url.slice(hashIndex + 1);

    if (fragment.startsWith('branch:')) {
      return { base, fragment, branch: fragment.slice(7) };
    }

    return { base, fragment, branch: fragment };
  }
};

/**
 * GitMsg Hash operations - handles commit hash operations
 */
export const gitMsgHash = {
  /**
   * Normalize hash to exactly 12 characters
   */
  normalize(hash: string): string {
    if (!hash || !/^[a-f0-9]+$/i.test(hash)) {
      throw new Error(`Invalid commit hash format: ${hash}`);
    }
    return hash.toLowerCase().substring(0, 12);
  },

  /**
   * Truncate hash to specified length (max 12)
   */
  truncate(hash: string, length: number): string {
    const normalized = this.normalize(hash);
    const maxLength = Math.min(length, 12);
    return normalized.substring(0, maxLength);
  },

  /**
   * Validate if hash is properly formatted (12 hex chars)
   */
  validate(hash: string): boolean {
    return !!hash && /^[a-f0-9]{12}$/i.test(hash);
  }
};
