import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { getLogLevel, log, type LogLevel } from './index';

describe('logger', () => {
  let originalEnv: string | undefined;
  let consoleLogSpy: ReturnType<typeof vi.spyOn>;
  let consoleErrorSpy: ReturnType<typeof vi.spyOn>;
  let consoleWarnSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    originalEnv = process.env['GITSOCIAL_LOG_LEVEL'];
    consoleLogSpy = vi.spyOn(console, 'log').mockImplementation(() => {});
    consoleErrorSpy = vi.spyOn(console, 'error').mockImplementation(() => {});
    consoleWarnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
  });

  afterEach(() => {
    if (originalEnv !== undefined) {
      process.env['GITSOCIAL_LOG_LEVEL'] = originalEnv;
    } else {
      delete process.env['GITSOCIAL_LOG_LEVEL'];
    }
    consoleLogSpy.mockRestore();
    consoleErrorSpy.mockRestore();
    consoleWarnSpy.mockRestore();
  });

  describe('getLogLevel()', () => {
    it('should return off by default', () => {
      delete process.env['GITSOCIAL_LOG_LEVEL'];
      expect(getLogLevel()).toBe('off');
    });

    it('should return error when set', () => {
      process.env['GITSOCIAL_LOG_LEVEL'] = 'error';
      expect(getLogLevel()).toBe('error');
    });

    it('should return warn when set', () => {
      process.env['GITSOCIAL_LOG_LEVEL'] = 'warn';
      expect(getLogLevel()).toBe('warn');
    });

    it('should return info when set', () => {
      process.env['GITSOCIAL_LOG_LEVEL'] = 'info';
      expect(getLogLevel()).toBe('info');
    });

    it('should return debug when set', () => {
      process.env['GITSOCIAL_LOG_LEVEL'] = 'debug';
      expect(getLogLevel()).toBe('debug');
    });

    it('should return verbose when set', () => {
      process.env['GITSOCIAL_LOG_LEVEL'] = 'verbose';
      expect(getLogLevel()).toBe('verbose');
    });

    it('should return off for invalid level', () => {
      process.env['GITSOCIAL_LOG_LEVEL'] = 'invalid';
      expect(getLogLevel()).toBe('off');
    });

    it('should return off for empty string', () => {
      process.env['GITSOCIAL_LOG_LEVEL'] = '';
      expect(getLogLevel()).toBe('off');
    });
  });

  describe('log()', () => {
    describe('with level off', () => {
      beforeEach(() => {
        process.env['GITSOCIAL_LOG_LEVEL'] = 'off';
      });

      it('should not log error', () => {
        log('error', 'test message');
        expect(consoleErrorSpy).not.toHaveBeenCalled();
      });

      it('should not log warn', () => {
        log('warn', 'test message');
        expect(consoleWarnSpy).not.toHaveBeenCalled();
      });

      it('should not log info', () => {
        log('info', 'test message');
        expect(consoleLogSpy).not.toHaveBeenCalled();
      });

      it('should not log debug', () => {
        log('debug', 'test message');
        expect(consoleLogSpy).not.toHaveBeenCalled();
      });

      it('should not log verbose', () => {
        log('verbose', 'test message');
        expect(consoleLogSpy).not.toHaveBeenCalled();
      });
    });

    describe('with level error', () => {
      beforeEach(() => {
        process.env['GITSOCIAL_LOG_LEVEL'] = 'error';
      });

      it('should log error', () => {
        log('error', 'test error');
        expect(consoleErrorSpy).toHaveBeenCalled();
        const call = consoleErrorSpy.mock.calls[0];
        expect(call?.[0]).toContain('[GitSocial]');
        expect(call?.[0]).toContain('[ERROR]');
        expect(call?.[0]).toContain('test error');
      });

      it('should not log warn', () => {
        log('warn', 'test message');
        expect(consoleWarnSpy).not.toHaveBeenCalled();
      });

      it('should not log info', () => {
        log('info', 'test message');
        expect(consoleLogSpy).not.toHaveBeenCalled();
      });
    });

    describe('with level warn', () => {
      beforeEach(() => {
        process.env['GITSOCIAL_LOG_LEVEL'] = 'warn';
      });

      it('should log error', () => {
        log('error', 'test error');
        expect(consoleErrorSpy).toHaveBeenCalled();
      });

      it('should log warn', () => {
        log('warn', 'test warning');
        expect(consoleWarnSpy).toHaveBeenCalled();
        const call = consoleWarnSpy.mock.calls[0];
        expect(call?.[0]).toContain('[GitSocial]');
        expect(call?.[0]).toContain('[WARN]');
        expect(call?.[0]).toContain('test warning');
      });

      it('should not log info', () => {
        log('info', 'test message');
        expect(consoleLogSpy).not.toHaveBeenCalled();
      });
    });

    describe('with level info', () => {
      beforeEach(() => {
        process.env['GITSOCIAL_LOG_LEVEL'] = 'info';
      });

      it('should log error', () => {
        log('error', 'test error');
        expect(consoleErrorSpy).toHaveBeenCalled();
      });

      it('should log warn', () => {
        log('warn', 'test warning');
        expect(consoleWarnSpy).toHaveBeenCalled();
      });

      it('should log info', () => {
        log('info', 'test info');
        expect(consoleLogSpy).toHaveBeenCalled();
        const call = consoleLogSpy.mock.calls[0];
        expect(call?.[0]).toContain('[GitSocial]');
        expect(call?.[0]).toContain('[INFO]');
        expect(call?.[0]).toContain('test info');
      });

      it('should not log debug', () => {
        log('debug', 'test message');
        expect(consoleLogSpy).not.toHaveBeenCalled();
      });
    });

    describe('with level debug', () => {
      beforeEach(() => {
        process.env['GITSOCIAL_LOG_LEVEL'] = 'debug';
      });

      it('should log error', () => {
        log('error', 'test error');
        expect(consoleErrorSpy).toHaveBeenCalled();
      });

      it('should log warn', () => {
        log('warn', 'test warning');
        expect(consoleWarnSpy).toHaveBeenCalled();
      });

      it('should log info', () => {
        log('info', 'test info');
        expect(consoleLogSpy).toHaveBeenCalled();
      });

      it('should log debug', () => {
        log('debug', 'test debug');
        expect(consoleLogSpy).toHaveBeenCalled();
        const call = consoleLogSpy.mock.calls[0];
        expect(call?.[0]).toContain('[GitSocial]');
        expect(call?.[0]).toContain('[DEBUG]');
        expect(call?.[0]).toContain('test debug');
      });

      it('should not log verbose', () => {
        log('verbose', 'test message');
        expect(consoleLogSpy).not.toHaveBeenCalled();
      });
    });

    describe('with level verbose', () => {
      beforeEach(() => {
        process.env['GITSOCIAL_LOG_LEVEL'] = 'verbose';
      });

      it('should log error', () => {
        log('error', 'test error');
        expect(consoleErrorSpy).toHaveBeenCalled();
      });

      it('should log warn', () => {
        log('warn', 'test warning');
        expect(consoleWarnSpy).toHaveBeenCalled();
      });

      it('should log info', () => {
        log('info', 'test info');
        expect(consoleLogSpy).toHaveBeenCalled();
      });

      it('should log debug', () => {
        log('debug', 'test debug');
        expect(consoleLogSpy).toHaveBeenCalled();
      });

      it('should log verbose', () => {
        log('verbose', 'test verbose');
        expect(consoleLogSpy).toHaveBeenCalled();
        const call = consoleLogSpy.mock.calls[0];
        expect(call?.[0]).toContain('[GitSocial]');
        expect(call?.[0]).toContain('[VERBOSE]');
        expect(call?.[0]).toContain('test verbose');
      });
    });

    describe('message formatting', () => {
      beforeEach(() => {
        process.env['GITSOCIAL_LOG_LEVEL'] = 'debug';
      });

      it('should include timestamp', () => {
        log('info', 'test message');
        const call = consoleLogSpy.mock.calls[0];
        expect(call?.[0]).toMatch(/\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}/);
      });

      it('should include GitSocial prefix', () => {
        log('info', 'test message');
        const call = consoleLogSpy.mock.calls[0];
        expect(call?.[0]).toContain('[GitSocial]');
      });

      it('should include log level', () => {
        log('info', 'test message');
        const call = consoleLogSpy.mock.calls[0];
        expect(call?.[0]).toContain('[INFO]');
      });

      it('should include message', () => {
        log('info', 'test message');
        const call = consoleLogSpy.mock.calls[0];
        expect(call?.[0]).toContain('test message');
      });

      it('should handle additional arguments', () => {
        log('info', 'test message', { foo: 'bar' }, 123);
        const call = consoleLogSpy.mock.calls[0];
        expect(call?.[1]).toEqual({ foo: 'bar' });
        expect(call?.[2]).toBe(123);
      });

      it('should handle empty message', () => {
        log('info', '');
        expect(consoleLogSpy).toHaveBeenCalled();
      });

      it('should handle multiline message', () => {
        log('info', 'line1\nline2\nline3');
        const call = consoleLogSpy.mock.calls[0];
        expect(call?.[0]).toContain('line1\nline2\nline3');
      });

      it('should handle objects in arguments', () => {
        const obj = { nested: { value: 'test' } };
        log('info', 'message', obj);
        const call = consoleLogSpy.mock.calls[0];
        expect(call?.[1]).toEqual(obj);
      });

      it('should handle arrays in arguments', () => {
        const arr = [1, 2, 3];
        log('info', 'message', arr);
        const call = consoleLogSpy.mock.calls[0];
        expect(call?.[1]).toEqual(arr);
      });
    });

    describe('log levels hierarchy', () => {
      const testCases: Array<[LogLevel, LogLevel[]]> = [
        ['error', ['error']],
        ['warn', ['error', 'warn']],
        ['info', ['error', 'warn', 'info']],
        ['debug', ['error', 'warn', 'info', 'debug']],
        ['verbose', ['error', 'warn', 'info', 'debug', 'verbose']]
      ];

      testCases.forEach(([setLevel, allowedLevels]) => {
        it(`should respect ${setLevel} level hierarchy`, () => {
          process.env['GITSOCIAL_LOG_LEVEL'] = setLevel;
          const allLevels: LogLevel[] = ['error', 'warn', 'info', 'debug', 'verbose'];
          allLevels.forEach(level => {
            consoleLogSpy.mockClear();
            consoleErrorSpy.mockClear();
            consoleWarnSpy.mockClear();
            log(level, 'test');
            const shouldHaveCalled = allowedLevels.includes(level);
            const calls = consoleLogSpy.mock.calls.length +
              consoleErrorSpy.mock.calls.length +
              consoleWarnSpy.mock.calls.length;
            expect(calls).toBe(shouldHaveCalled ? 1 : 0);
          });
        });
      });
    });
  });
});
