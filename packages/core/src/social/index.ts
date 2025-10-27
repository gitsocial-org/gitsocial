/**
 * GitSocial core social features
 */

// Import namespace objects
import { post } from './post';
import { cache } from './post/cache';
import { list } from './list';
import { interaction } from './post/interaction';
import { avatar } from './avatar';
import { repository } from './repository';
import { search } from './search';
import { thread } from './thread';
import { timeline } from './timeline';
import { log } from './log';
import { follower } from './follower';
import { notification } from './notification';

/**
 * Social namespace - Core social features
 */
export const social = {
  post,
  search,
  interaction,
  thread,
  timeline,
  log,
  cache,
  list,
  avatar,
  repository,
  follower,
  notification
};

// Export types using wildcards (compile-time only)
export type * from './types';
export type * from './errors';
export type * from './search';
export type * from './timeline';
export type * from './avatar/types';
export type * from './follower';
export type * from './notification';
