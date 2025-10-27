import {
  clearAvatarCache,
  getAvatar,
  getAvatarCacheStats,
  setEnableGravatar,
  setGitHubToken
} from './service';

/**
 * Avatar namespace
 */
export const avatar = {
  getAvatar,
  clearAvatarCache,
  getAvatarCacheStats,
  setGitHubToken,
  setEnableGravatar
};

export * from './types';
