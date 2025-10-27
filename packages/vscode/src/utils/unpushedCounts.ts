import { git } from '@gitsocial/core';

export interface UnpushedCounts {
  posts: number;
  comments: number;
  total: number;
}

/**
 * Get counts of unpushed GitSocial posts and comments
 * Reads directly from Git to ensure accuracy
 */
export async function getUnpushedCounts(repository: string): Promise<UnpushedCounts> {
  try {
    const gitSocialBranch = await git.getConfiguredBranch(repository);

    const commits = await git.getCommits(repository, {
      branch: gitSocialBranch,
      limit: 10000
    });

    if (!commits || commits.length === 0) {
      return { posts: 0, comments: 0, total: 0 };
    }

    // Get unpushed commits
    const unpushedCommits = await git.getUnpushedCommits(repository, gitSocialBranch);

    // Filter for GitSocial posts and count by type
    let unpushedPosts = 0;
    let unpushedComments = 0;

    for (const commit of commits) {
      const shortHash = commit.hash.substring(0, 12);
      const isUnpushed = unpushedCommits.has(shortHash);

      if (!isUnpushed) {
        continue;
      }

      // All unpushed commits on the GitSocial branch are GitSocial posts
      // Determine type by checking for markers
      if (commit.message.includes('GitMsg-ReplyTo')) {
        unpushedComments++;
      } else {
        unpushedPosts++;
      }
    }

    return {
      posts: unpushedPosts,
      comments: unpushedComments,
      total: unpushedPosts + unpushedComments
    };
  } catch {
    return { posts: 0, comments: 0, total: 0 };
  }
}
