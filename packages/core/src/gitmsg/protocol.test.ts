import { describe, expect, it } from 'vitest';
import { gitMsgHash, gitMsgRef, gitMsgUrl } from './protocol';

describe('gitMsgRef', () => {
  describe('create()', () => {
    it('should create relative commit ref', () => {
      const ref = gitMsgRef.create('commit', 'abc123def456789');
      expect(ref).toBe('#commit:abc123def456');
    });

    it('should create absolute commit ref', () => {
      const ref = gitMsgRef.create('commit', 'abc123def456', 'https://github.com/user/repo');
      expect(ref).toBe('https://github.com/user/repo#commit:abc123def456');
    });

    it('should normalize commit hash to 12 characters', () => {
      const ref = gitMsgRef.create('commit', 'abc123');
      expect(ref).toBe('#commit:abc123');

      const longRef = gitMsgRef.create('commit', 'abc123def456789012345678');
      expect(longRef).toBe('#commit:abc123def456');
    });

    it('should lowercase commit hashes', () => {
      const ref = gitMsgRef.create('commit', 'ABC123DEF456');
      expect(ref).toBe('#commit:abc123def456');
    });

    it('should create branch ref', () => {
      const ref = gitMsgRef.create('branch', 'main');
      expect(ref).toBe('#branch:main');
    });

    it('should create absolute branch ref', () => {
      const ref = gitMsgRef.create('branch', 'main', 'https://github.com/user/repo');
      expect(ref).toBe('https://github.com/user/repo#branch:main');
    });

    it('should create list ref', () => {
      const ref = gitMsgRef.create('list', 'reading');
      expect(ref).toBe('#list:reading');
    });
  });

  describe('parse()', () => {
    it('should parse relative commit ref', () => {
      const result = gitMsgRef.parse('#commit:abc123def456');
      expect(result).toEqual({
        type: 'commit',
        repository: undefined,
        value: 'abc123def456'
      });
    });

    it('should parse absolute commit ref', () => {
      const result = gitMsgRef.parse('https://github.com/user/repo#commit:abc123def456');
      expect(result).toEqual({
        type: 'commit',
        repository: 'https://github.com/user/repo',
        value: 'abc123def456'
      });
    });

    it('should normalize URLs when parsing', () => {
      const result = gitMsgRef.parse('https://GitHub.com/user/repo.git#commit:abc123def456');
      expect(result.repository).toBe('https://github.com/user/repo');
    });

    it('should parse branch ref', () => {
      const result = gitMsgRef.parse('#branch:main');
      expect(result).toEqual({
        type: 'branch',
        repository: undefined,
        value: 'main'
      });
    });

    it('should parse absolute branch ref', () => {
      const result = gitMsgRef.parse('https://github.com/user/repo#branch:feature/new');
      expect(result).toEqual({
        type: 'branch',
        repository: 'https://github.com/user/repo',
        value: 'feature/new'
      });
    });

    it('should parse list ref', () => {
      const result = gitMsgRef.parse('#list:reading');
      expect(result).toEqual({
        type: 'list',
        repository: undefined,
        value: 'reading'
      });
    });

    it('should handle unknown ref types', () => {
      const result = gitMsgRef.parse('invalid-ref');
      expect(result).toEqual({
        type: 'unknown',
        value: 'invalid-ref'
      });
    });

    it('should truncate long commit hashes to 12 chars', () => {
      const result = gitMsgRef.parse('#commit:abc123def456789012345');
      expect(result.value).toBe('abc123def456');
    });
  });

  describe('validate()', () => {
    it('should validate commit refs', () => {
      expect(gitMsgRef.validate('#commit:abc123def456', 'commit')).toBe(true);
      expect(gitMsgRef.validate('https://github.com/user/repo#commit:abc123def456', 'commit')).toBe(true);
    });

    it('should reject invalid commit refs', () => {
      expect(gitMsgRef.validate('#commit:abc', 'commit')).toBe(false);
      expect(gitMsgRef.validate('#commit:XYZ123def456', 'commit')).toBe(false);
      expect(gitMsgRef.validate('#commit:abc123def456789', 'commit')).toBe(false);
    });

    it('should validate branch refs', () => {
      expect(gitMsgRef.validate('#branch:main', 'branch')).toBe(true);
      expect(gitMsgRef.validate('#branch:feature/new', 'branch')).toBe(true);
      expect(gitMsgRef.validate('https://github.com/user/repo#branch:main', 'branch')).toBe(true);
    });

    it('should validate list refs', () => {
      expect(gitMsgRef.validate('#list:reading', 'list')).toBe(true);
      expect(gitMsgRef.validate('#list:my-list_123', 'list')).toBe(true);
    });

    it('should reject list names over 40 chars', () => {
      const longName = 'a'.repeat(41);
      expect(gitMsgRef.validate(`#list:${longName}`, 'list')).toBe(false);
    });

    it('should validate any ref type when type not specified', () => {
      expect(gitMsgRef.validate('#commit:abc123def456')).toBe(true);
      expect(gitMsgRef.validate('#branch:main')).toBe(true);
      expect(gitMsgRef.validate('#list:reading')).toBe(true);
      expect(gitMsgRef.validate('invalid')).toBe(false);
    });

    it('should reject empty refs', () => {
      expect(gitMsgRef.validate('')).toBe(false);
    });
  });

  describe('validateListName()', () => {
    it('should validate valid list names', () => {
      expect(gitMsgRef.validateListName('reading')).toBe(true);
      expect(gitMsgRef.validateListName('my-list')).toBe(true);
      expect(gitMsgRef.validateListName('list_123')).toBe(true);
      expect(gitMsgRef.validateListName('a')).toBe(true);
    });

    it('should reject invalid characters', () => {
      expect(gitMsgRef.validateListName('my list')).toBe(false);
      expect(gitMsgRef.validateListName('list@home')).toBe(false);
      expect(gitMsgRef.validateListName('list.name')).toBe(false);
    });

    it('should reject names over 40 chars', () => {
      expect(gitMsgRef.validateListName('a'.repeat(40))).toBe(true);
      expect(gitMsgRef.validateListName('a'.repeat(41))).toBe(false);
    });

    it('should reject empty names', () => {
      expect(gitMsgRef.validateListName('')).toBe(false);
    });
  });

  describe('normalize()', () => {
    it('should normalize commit refs to 12 chars', () => {
      const ref = gitMsgRef.normalize('#commit:abc123def456789012');
      expect(ref).toBe('#commit:abc123def456');
    });

    it('should not modify non-commit refs', () => {
      expect(gitMsgRef.normalize('#branch:main')).toBe('#branch:main');
      expect(gitMsgRef.normalize('#list:reading')).toBe('#list:reading');
    });

    it('should handle empty refs', () => {
      expect(gitMsgRef.normalize('')).toBe('');
    });
  });

  describe('isMyRepository()', () => {
    it('should return true for relative refs', () => {
      expect(gitMsgRef.isMyRepository('#commit:abc123def456')).toBe(true);
      expect(gitMsgRef.isMyRepository('#branch:main')).toBe(true);
    });

    it('should return false for absolute refs', () => {
      expect(gitMsgRef.isMyRepository('https://github.com/user/repo#commit:abc123def456')).toBe(false);
    });
  });

  describe('parseRepositoryId()', () => {
    it('should parse repository with branch', () => {
      const result = gitMsgRef.parseRepositoryId('https://github.com/user/repo#branch:main');
      expect(result).toEqual({
        repository: 'https://github.com/user/repo',
        branch: 'main'
      });
    });

    it('should default to main branch when not specified', () => {
      const result = gitMsgRef.parseRepositoryId('https://github.com/user/repo');
      expect(result).toEqual({
        repository: 'https://github.com/user/repo',
        branch: 'main'
      });
    });

    it('should normalize repository URL', () => {
      const result = gitMsgRef.parseRepositoryId('https://GitHub.com/user/repo.git#branch:develop');
      expect(result.repository).toBe('https://github.com/user/repo');
      expect(result.branch).toBe('develop');
    });
  });

  describe('extractBranchFromRemote()', () => {
    it('should extract branch from remotes/ format', () => {
      expect(gitMsgRef.extractBranchFromRemote('remotes/origin/main')).toBe('main');
      expect(gitMsgRef.extractBranchFromRemote('remotes/upstream/feature/new')).toBe('feature/new');
    });

    it('should extract branch from remote/branch format', () => {
      expect(gitMsgRef.extractBranchFromRemote('origin/main')).toBe('main');
      expect(gitMsgRef.extractBranchFromRemote('upstream/develop')).toBe('develop');
    });

    it('should return unchanged if no remote prefix', () => {
      expect(gitMsgRef.extractBranchFromRemote('main')).toBe('main');
    });

    it('should return unchanged if remotes/ format has less than 3 parts', () => {
      expect(gitMsgRef.extractBranchFromRemote('remotes/origin')).toBe('remotes/origin');
      expect(gitMsgRef.extractBranchFromRemote('remotes/')).toBe('remotes/');
    });
  });

  describe('normalizeHashInRefWithContext()', () => {
    it('should add repository to relative refs', () => {
      const result = gitMsgRef.normalizeHashInRefWithContext(
        '#commit:abc123def456',
        'https://github.com/user/repo'
      );
      expect(result).toBe('https://github.com/user/repo#commit:abc123def456');
    });

    it('should not modify absolute refs', () => {
      const result = gitMsgRef.normalizeHashInRefWithContext(
        'https://github.com/other/repo#commit:abc123def456',
        'https://github.com/user/repo'
      );
      expect(result).toBe('https://github.com/other/repo#commit:abc123def456');
    });

    it('should remove .git from repository URL', () => {
      const result = gitMsgRef.normalizeHashInRefWithContext(
        '#commit:abc123def456',
        'https://github.com/user/repo.git'
      );
      expect(result).toBe('https://github.com/user/repo#commit:abc123def456');
    });

    it('should handle empty ref', () => {
      expect(gitMsgRef.normalizeHashInRefWithContext('', 'https://github.com/user/repo')).toBe('');
    });
  });
});

describe('gitMsgUrl', () => {
  describe('normalize()', () => {
    it('should remove .git suffix', () => {
      expect(gitMsgUrl.normalize('https://github.com/user/repo.git')).toBe('https://github.com/user/repo');
    });

    it('should lowercase hostname', () => {
      expect(gitMsgUrl.normalize('https://GitHub.com/user/repo')).toBe('https://github.com/user/repo');
    });

    it('should preserve path case', () => {
      expect(gitMsgUrl.normalize('https://github.com/User/Repo')).toBe('https://github.com/User/Repo');
    });

    it('should convert SSH to HTTPS', () => {
      expect(gitMsgUrl.normalize('git@github.com:user/repo')).toBe('https://github.com/user/repo');
      expect(gitMsgUrl.normalize('git@github.com:user/repo.git')).toBe('https://github.com/user/repo');
    });

    it('should trim whitespace', () => {
      expect(gitMsgUrl.normalize('  https://github.com/user/repo  ')).toBe('https://github.com/user/repo');
    });

    it('should handle empty URLs', () => {
      expect(gitMsgUrl.normalize('')).toBe('');
    });
  });

  describe('validate()', () => {
    it('should validate HTTPS URLs', () => {
      expect(gitMsgUrl.validate('https://github.com/user/repo')).toBe(true);
      expect(gitMsgUrl.validate('https://gitlab.com/group/project')).toBe(true);
    });

    it('should validate SSH URLs', () => {
      expect(gitMsgUrl.validate('git@github.com:user/repo')).toBe(true);
      expect(gitMsgUrl.validate('git@gitlab.com:group/project.git')).toBe(true);
    });

    it('should reject invalid URLs', () => {
      expect(gitMsgUrl.validate('')).toBe(false);
      expect(gitMsgUrl.validate('not-a-url')).toBe(false);
      expect(gitMsgUrl.validate('https://github.com')).toBe(false);
      expect(gitMsgUrl.validate('https://github.com/')).toBe(false);
    });

    it('should reject non-string values', () => {
      expect(gitMsgUrl.validate(null as unknown as string)).toBe(false);
      expect(gitMsgUrl.validate(undefined as unknown as string)).toBe(false);
    });

    it('should handle malformed HTTPS URLs gracefully', () => {
      expect(gitMsgUrl.validate('https://')).toBe(false);
      expect(gitMsgUrl.validate('https:// invalid')).toBe(false);
    });
  });

  describe('toGit()', () => {
    it('should add .git suffix', () => {
      expect(gitMsgUrl.toGit('https://github.com/user/repo')).toBe('https://github.com/user/repo.git');
    });

    it('should not duplicate .git suffix', () => {
      expect(gitMsgUrl.toGit('https://github.com/user/repo.git')).toBe('https://github.com/user/repo.git');
    });

    it('should handle empty URLs', () => {
      expect(gitMsgUrl.toGit('')).toBe('');
    });

    it('should trim whitespace', () => {
      expect(gitMsgUrl.toGit('  https://github.com/user/repo  ')).toBe('https://github.com/user/repo.git');
    });
  });

  describe('fromRef()', () => {
    it('should extract repository from absolute ref', () => {
      const url = gitMsgUrl.fromRef('https://github.com/user/repo#commit:abc123def456');
      expect(url).toBe('https://github.com/user/repo');
    });

    it('should return null for relative ref', () => {
      const url = gitMsgUrl.fromRef('#commit:abc123def456');
      expect(url).toBeNull();
    });

    it('should extract repository from branch ref', () => {
      const url = gitMsgUrl.fromRef('https://github.com/user/repo#branch:main');
      expect(url).toBe('https://github.com/user/repo');
    });

    it('should extract repository from list ref', () => {
      const url = gitMsgUrl.fromRef('https://github.com/user/repo#list:reading');
      expect(url).toBe('https://github.com/user/repo');
    });
  });

  describe('parseFragment()', () => {
    it('should parse URL without fragment', () => {
      const result = gitMsgUrl.parseFragment('https://github.com/user/repo');
      expect(result).toEqual({ base: 'https://github.com/user/repo' });
    });

    it('should parse URL with branch: prefix', () => {
      const result = gitMsgUrl.parseFragment('https://github.com/user/repo#branch:main');
      expect(result).toEqual({
        base: 'https://github.com/user/repo',
        fragment: 'branch:main',
        branch: 'main'
      });
    });

    it('should parse URL with plain fragment', () => {
      const result = gitMsgUrl.parseFragment('https://github.com/user/repo#develop');
      expect(result).toEqual({
        base: 'https://github.com/user/repo',
        fragment: 'develop',
        branch: 'develop'
      });
    });

    it('should handle commit fragments', () => {
      const result = gitMsgUrl.parseFragment('https://github.com/user/repo#commit:abc123');
      expect(result).toEqual({
        base: 'https://github.com/user/repo',
        fragment: 'commit:abc123',
        branch: 'commit:abc123'
      });
    });
  });
});

describe('gitMsgHash', () => {
  describe('normalize()', () => {
    it('should normalize valid hash to 12 characters', () => {
      expect(gitMsgHash.normalize('abc123def456789012345678')).toBe('abc123def456');
    });

    it('should lowercase hash', () => {
      expect(gitMsgHash.normalize('ABC123DEF456')).toBe('abc123def456');
    });

    it('should handle exactly 12 character hash', () => {
      expect(gitMsgHash.normalize('abc123def456')).toBe('abc123def456');
    });

    it('should throw error for invalid hash format', () => {
      expect(() => gitMsgHash.normalize('xyz123')).toThrow('Invalid commit hash format');
      expect(() => gitMsgHash.normalize('abc123-def')).toThrow('Invalid commit hash format');
      expect(() => gitMsgHash.normalize('')).toThrow('Invalid commit hash format');
      expect(() => gitMsgHash.normalize('not-a-hash')).toThrow('Invalid commit hash format');
    });
  });

  describe('truncate()', () => {
    it('should truncate hash to specified length', () => {
      expect(gitMsgHash.truncate('abc123def456789', 6)).toBe('abc123');
      expect(gitMsgHash.truncate('abc123def456789', 8)).toBe('abc123de');
    });

    it('should respect max length of 12', () => {
      expect(gitMsgHash.truncate('abc123def456789', 20)).toBe('abc123def456');
    });

    it('should handle length of 12', () => {
      expect(gitMsgHash.truncate('abc123def456789', 12)).toBe('abc123def456');
    });

    it('should normalize before truncating', () => {
      expect(gitMsgHash.truncate('ABC123DEF456789', 6)).toBe('abc123');
    });
  });

  describe('validate()', () => {
    it('should validate correct 12-char hex hash', () => {
      expect(gitMsgHash.validate('abc123def456')).toBe(true);
      expect(gitMsgHash.validate('ABC123DEF456')).toBe(true);
      expect(gitMsgHash.validate('000000000000')).toBe(true);
    });

    it('should reject invalid hashes', () => {
      expect(gitMsgHash.validate('abc123')).toBe(false);
      expect(gitMsgHash.validate('abc123def456789')).toBe(false);
      expect(gitMsgHash.validate('xyz123def456')).toBe(false);
      expect(gitMsgHash.validate('abc123-def45')).toBe(false);
      expect(gitMsgHash.validate('')).toBe(false);
      expect(gitMsgHash.validate('not-a-hash')).toBe(false);
    });
  });
});
