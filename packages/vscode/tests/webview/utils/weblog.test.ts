import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { getWebLogLevel, setWebLogLevel, webLog } from '../../../src/webview/utils/weblog';

describe('Weblog utilities', () => {
  let consoleErrorSpy: ReturnType<typeof vi.spyOn>;
  let consoleWarnSpy: ReturnType<typeof vi.spyOn>;
  let consoleLogSpy: ReturnType<typeof vi.spyOn>;
  let localStorageMock: Record<string, string>;

  beforeEach(() => {
    consoleErrorSpy = vi.spyOn(console, 'error').mockImplementation(() => undefined);
    consoleWarnSpy = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    consoleLogSpy = vi.spyOn(console, 'log').mockImplementation(() => undefined);

    localStorageMock = {};
    global.localStorage = {
      getItem: vi.fn((key: string) => localStorageMock[key] || null),
      setItem: vi.fn((key: string, value: string) => { localStorageMock[key] = value; }),
      removeItem: vi.fn((key: string) => { delete localStorageMock[key]; }),
      clear: vi.fn(() => { localStorageMock = {}; }),
      key: vi.fn(),
      length: 0
    };
  });

  afterEach(() => {
    consoleErrorSpy.mockRestore();
    consoleWarnSpy.mockRestore();
    consoleLogSpy.mockRestore();
  });

  describe('webLog', () => {
    it('logs error messages', () => {
      setWebLogLevel('error');
      webLog('error', 'Test error', { data: 'value' });
      expect(consoleErrorSpy).toHaveBeenCalled();
      expect(consoleErrorSpy.mock.calls[0][0]).toMatch(/ERROR.*Test error/);
    });

    it('logs warn messages', () => {
      setWebLogLevel('warn');
      webLog('warn', 'Test warning');
      expect(consoleWarnSpy).toHaveBeenCalled();
      expect(consoleWarnSpy.mock.calls[0][0]).toMatch(/WARN.*Test warning/);
    });

    it('logs info messages', () => {
      setWebLogLevel('info');
      webLog('info', 'Test info');
      expect(consoleLogSpy).toHaveBeenCalled();
      expect(consoleLogSpy.mock.calls[0][0]).toMatch(/INFO.*Test info/);
    });

    it('logs debug messages', () => {
      setWebLogLevel('debug');
      webLog('debug', 'Test debug');
      expect(consoleLogSpy).toHaveBeenCalled();
      expect(consoleLogSpy.mock.calls[0][0]).toMatch(/DEBUG.*Test debug/);
    });

    it('logs verbose messages', () => {
      setWebLogLevel('verbose');
      webLog('verbose', 'Test verbose');
      expect(consoleLogSpy).toHaveBeenCalled();
      expect(consoleLogSpy.mock.calls[0][0]).toMatch(/VERBOSE.*Test verbose/);
    });

    it('includes additional arguments', () => {
      setWebLogLevel('error');
      const arg1 = { foo: 'bar' };
      const arg2 = [1, 2, 3];
      webLog('error', 'Test', arg1, arg2);
      expect(consoleErrorSpy).toHaveBeenCalledWith(
        expect.stringMatching(/Test/),
        arg1,
        arg2
      );
    });

    it('does not log when level is off', () => {
      setWebLogLevel('off');
      webLog('error', 'Should not log');
      webLog('warn', 'Should not log');
      webLog('info', 'Should not log');
      expect(consoleErrorSpy).not.toHaveBeenCalled();
      expect(consoleWarnSpy).not.toHaveBeenCalled();
      expect(consoleLogSpy).not.toHaveBeenCalled();
    });

    it('respects log level hierarchy', () => {
      setWebLogLevel('warn');
      webLog('error', 'Error - should log');
      webLog('warn', 'Warn - should log');
      webLog('info', 'Info - should not log');
      webLog('debug', 'Debug - should not log');

      expect(consoleErrorSpy).toHaveBeenCalledTimes(1);
      expect(consoleWarnSpy).toHaveBeenCalledTimes(1);
      expect(consoleLogSpy).not.toHaveBeenCalled();
    });

    it('formats message with timestamp', () => {
      setWebLogLevel('error');
      webLog('error', 'Test');
      const message = consoleErrorSpy.mock.calls[0][0];
      expect(message).toMatch(/\[GitSocial Web\]/);
      expect(message).toMatch(/\[\d{1,2}:\d{2}:\d{2}/); // Timestamp pattern
      expect(message).toMatch(/\[ERROR\]/);
    });
  });

  describe('setWebLogLevel', () => {
    it('stores level in localStorage', () => {
      setWebLogLevel('debug');
      // eslint-disable-next-line @typescript-eslint/unbound-method
      const setItem = localStorage.setItem;
      expect(setItem).toHaveBeenCalledWith('gitsocial-weblog-level', 'debug');
      expect(localStorageMock['gitsocial-weblog-level']).toBe('debug');
    });

    it('handles localStorage errors gracefully', () => {
      global.localStorage.setItem = vi.fn(() => {
        throw new Error('localStorage not available');
      });

      expect(() => setWebLogLevel('debug')).not.toThrow();
    });
  });

  describe('getWebLogLevel', () => {
    it('returns stored level from localStorage', () => {
      setWebLogLevel('verbose');
      expect(getWebLogLevel()).toBe('verbose');
    });

    it('returns default level when nothing stored', () => {
      expect(getWebLogLevel()).toBe('error');
    });

    it('returns default level for invalid stored value', () => {
      localStorageMock['gitsocial-weblog-level'] = 'invalid';
      expect(getWebLogLevel()).toBe('error');
    });

    it('returns default level when localStorage throws', () => {
      global.localStorage.getItem = vi.fn(() => {
        throw new Error('localStorage not available');
      });
      expect(getWebLogLevel()).toBe('error');
    });
  });
});
