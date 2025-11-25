import { describe, expect, it } from 'vitest';
import { avatarUtils } from '../../../src/social/avatar/utils';

describe('social/avatar/utils', () => {
  describe('md5Hash()', () => {
    it('should generate md5 hash for string', () => {
      const hash = avatarUtils.md5Hash('test@example.com');

      expect(hash).toHaveLength(32);
      expect(hash).toMatch(/^[0-9a-f]{32}$/);
    });

    it('should lowercase and trim input', () => {
      const hash1 = avatarUtils.md5Hash('TEST@EXAMPLE.COM');
      const hash2 = avatarUtils.md5Hash('  test@example.com  ');

      expect(hash1).toBe(hash2);
    });

    it('should generate consistent hashes', () => {
      const hash1 = avatarUtils.md5Hash('user@domain.com');
      const hash2 = avatarUtils.md5Hash('user@domain.com');

      expect(hash1).toBe(hash2);
    });

    it('should generate different hashes for different inputs', () => {
      const hash1 = avatarUtils.md5Hash('user1@domain.com');
      const hash2 = avatarUtils.md5Hash('user2@domain.com');

      expect(hash1).not.toBe(hash2);
    });
  });

  describe('extractGitHubUsername()', () => {
    it('should extract username from GitHub noreply email', () => {
      const letter = avatarUtils.extractGitHubUsername('123456+testuser@users.noreply.github.com');

      expect(letter).toBe('T');
    });

    it('should extract first letter from regular email', () => {
      const letter = avatarUtils.extractGitHubUsername('john.doe@example.com');

      expect(letter).toBe('J');
    });

    it('should extract first letter from repository URL', () => {
      const letter = avatarUtils.extractGitHubUsername('github.com/user/myrepo');

      expect(letter).toBe('M');
    });

    it('should handle URL path', () => {
      const letter = avatarUtils.extractGitHubUsername('github.com/user/repo');

      expect(letter).toBe('R');
    });

    it('should return first character for simple string', () => {
      const letter = avatarUtils.extractGitHubUsername('username');

      expect(letter).toBe('U');
    });

    it('should return ? for empty string', () => {
      const letter = avatarUtils.extractGitHubUsername('');

      expect(letter).toBe('?');
    });

    it('should uppercase the first letter', () => {
      const letter = avatarUtils.extractGitHubUsername('abc@example.com');

      expect(letter).toBe('A');
    });
  });

  describe('createLetterAvatarSvg()', () => {
    it('should use initials when name is empty string', () => {
      const svg = avatarUtils.createLetterAvatarSvg('test@example.com', 64, '');

      expect(svg).toContain('<svg');
      expect(svg).toContain('T');
    });

    it('should create SVG with extracted letter from identifier', () => {
      const svg = avatarUtils.createLetterAvatarSvg('test@example.com', 64);

      expect(svg).toContain('<svg');
      expect(svg).toContain('width="64"');
      expect(svg).toContain('height="64"');
      expect(svg).toContain('<circle');
      expect(svg).toContain('<text');
      expect(svg).toContain('T');
    });

    it('should use initials from name when provided', () => {
      const svg = avatarUtils.createLetterAvatarSvg('test@example.com', 64, 'John Doe');

      expect(svg).toContain('JD');
    });

    it('should use color based on identifier hash', () => {
      const svg = avatarUtils.createLetterAvatarSvg('test@example.com', 64);
      const hash = avatarUtils.md5Hash('test@example.com').substring(0, 6);

      expect(svg).toContain(`fill="#${hash}"`);
    });

    it('should create different sizes', () => {
      const svg32 = avatarUtils.createLetterAvatarSvg('test@example.com', 32);
      const svg128 = avatarUtils.createLetterAvatarSvg('test@example.com', 128);

      expect(svg32).toContain('width="32"');
      expect(svg128).toContain('width="128"');
    });

    it('should adjust font size for single letter', () => {
      const svg = avatarUtils.createLetterAvatarSvg('test@example.com', 100, 'A');

      expect(svg).toContain('font-size="40"');
    });

    it('should adjust font size for two letters', () => {
      const svg = avatarUtils.createLetterAvatarSvg('test@example.com', 100, 'AB CD');

      expect(svg).toContain('font-size="33"');
    });
  });

  describe('createHomeIconAvatarSvg()', () => {
    it('should create SVG with home icon', () => {
      const svg = avatarUtils.createHomeIconAvatarSvg(64);

      expect(svg).toContain('<svg');
      expect(svg).toContain('width="64"');
      expect(svg).toContain('height="64"');
      expect(svg).toContain('<circle');
      expect(svg).toContain('<path');
    });

    it('should use neutral gray color', () => {
      const svg = avatarUtils.createHomeIconAvatarSvg(64);

      expect(svg).toContain('fill="#666666"');
    });

    it('should scale icon based on size', () => {
      const svg32 = avatarUtils.createHomeIconAvatarSvg(32);
      const svg128 = avatarUtils.createHomeIconAvatarSvg(128);

      expect(svg32).toContain('width="32"');
      expect(svg128).toContain('width="128"');
    });

    it('should center icon with proper transform', () => {
      const svg = avatarUtils.createHomeIconAvatarSvg(64);

      expect(svg).toContain('transform=');
      expect(svg).toContain('scale(');
    });

    it('should include proper SVG viewBox', () => {
      const svg = avatarUtils.createHomeIconAvatarSvg(100);

      expect(svg).toContain('viewBox="0 0 100 100"');
    });
  });
});
