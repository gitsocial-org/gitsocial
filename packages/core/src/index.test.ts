import { describe, expect, it } from 'vitest';
import * as coreExports from './index';

describe('index', () => {
  it('should export git modules', () => {
    expect(coreExports).toHaveProperty('git');
    expect(typeof coreExports.git).toBe('object');
  });

  it('should export gitmsg modules', () => {
    expect(coreExports).toHaveProperty('gitMsg');
    expect(coreExports).toHaveProperty('gitMsgRef');
    expect(coreExports).toHaveProperty('gitMsgUrl');
    expect(coreExports).toHaveProperty('gitMsgHash');
  });

  it('should export social modules', () => {
    expect(coreExports).toHaveProperty('social');
  });

  it('should export storage modules', () => {
    expect(coreExports).toHaveProperty('storage');
  });

  it('should export logger', () => {
    expect(coreExports).toHaveProperty('log');
  });
});
