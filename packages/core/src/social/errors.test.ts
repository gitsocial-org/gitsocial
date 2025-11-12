import { describe, expect, it } from 'vitest';
import { SOCIAL_ERROR_CODES, type SocialErrorCode } from './errors';

describe('social/errors', () => {
  describe('SOCIAL_ERROR_CODES', () => {
    it('should export all social layer error codes', () => {
      expect(SOCIAL_ERROR_CODES.SOCIAL_ERROR).toBe('SOCIAL_ERROR');
      expect(SOCIAL_ERROR_CODES.LIST_NOT_FOUND).toBe('LIST_NOT_FOUND');
      expect(SOCIAL_ERROR_CODES.LIST_EXISTS).toBe('LIST_EXISTS');
      expect(SOCIAL_ERROR_CODES.INVALID_LIST_NAME).toBe('INVALID_LIST_NAME');
      expect(SOCIAL_ERROR_CODES.POST_NOT_FOUND).toBe('POST_NOT_FOUND');
      expect(SOCIAL_ERROR_CODES.REPOSITORY_NOT_FOUND).toBe('REPOSITORY_NOT_FOUND');
      expect(SOCIAL_ERROR_CODES.ALREADY_FOLLOWING).toBe('ALREADY_FOLLOWING');
      expect(SOCIAL_ERROR_CODES.NOT_FOLLOWING).toBe('NOT_FOLLOWING');
      expect(SOCIAL_ERROR_CODES.MISSING_CONTENT).toBe('MISSING_CONTENT');
      expect(SOCIAL_ERROR_CODES.INTERACTION_ERROR).toBe('INTERACTION_ERROR');
      expect(SOCIAL_ERROR_CODES.SEARCH_ERROR).toBe('SEARCH_ERROR');
      expect(SOCIAL_ERROR_CODES.CONFIG_ERROR).toBe('CONFIG_ERROR');
    });

    it('should export all generic operation error codes', () => {
      expect(SOCIAL_ERROR_CODES.CREATE_ERROR).toBe('CREATE_ERROR');
      expect(SOCIAL_ERROR_CODES.DELETE_ERROR).toBe('DELETE_ERROR');
      expect(SOCIAL_ERROR_CODES.UPDATE_ERROR).toBe('UPDATE_ERROR');
      expect(SOCIAL_ERROR_CODES.VALIDATION_ERROR).toBe('VALIDATION_ERROR');
      expect(SOCIAL_ERROR_CODES.PERMISSION_ERROR).toBe('PERMISSION_ERROR');
      expect(SOCIAL_ERROR_CODES.NETWORK_ERROR).toBe('NETWORK_ERROR');
      expect(SOCIAL_ERROR_CODES.TIMEOUT_ERROR).toBe('TIMEOUT_ERROR');
    });

    it('should have correct structure', () => {
      expect(Object.keys(SOCIAL_ERROR_CODES).length).toBe(19);
      expect(typeof SOCIAL_ERROR_CODES).toBe('object');
    });

    it('should have correct type for SocialErrorCode', () => {
      const code: SocialErrorCode = 'SOCIAL_ERROR';
      expect(code).toBe('SOCIAL_ERROR');

      const codes: SocialErrorCode[] = [
        'SOCIAL_ERROR',
        'LIST_NOT_FOUND',
        'LIST_EXISTS',
        'INVALID_LIST_NAME',
        'POST_NOT_FOUND',
        'REPOSITORY_NOT_FOUND',
        'ALREADY_FOLLOWING',
        'NOT_FOLLOWING',
        'MISSING_CONTENT',
        'INTERACTION_ERROR',
        'SEARCH_ERROR',
        'CONFIG_ERROR',
        'CREATE_ERROR',
        'DELETE_ERROR',
        'UPDATE_ERROR',
        'VALIDATION_ERROR',
        'PERMISSION_ERROR',
        'NETWORK_ERROR',
        'TIMEOUT_ERROR'
      ];
      expect(codes).toHaveLength(19);
    });
  });
});
