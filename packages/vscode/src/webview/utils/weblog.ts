/**
 * Frontend logger for GitSocial webview components
 * Simple console-based logging with configurable levels
 */

import type { LogLevel } from '@gitsocial/core';

// Re-export type for use in webview
export type { LogLevel };

const LOG_LEVELS: Record<LogLevel, number> = {
  off: 0,
  error: 1,
  warn: 2,
  info: 3,
  debug: 4,
  verbose: 5
};

const STORAGE_KEY = 'gitsocial-weblog-level';
const DEFAULT_LEVEL: LogLevel = 'error';

function getCurrentLevel(): LogLevel {
  try {
    const stored = localStorage.getItem(STORAGE_KEY);
    if (stored && isValidLevel(stored)) {
      return stored as LogLevel;
    }
  } catch {
    // localStorage not available or error
  }
  return DEFAULT_LEVEL;
}

function isValidLevel(level: string): boolean {
  return Object.keys(LOG_LEVELS).includes(level);
}

function shouldLog(level: LogLevel): boolean {
  const currentLevel = getCurrentLevel();
  return LOG_LEVELS[level] <= LOG_LEVELS[currentLevel];
}

function formatMessage(level: string, message: string): string {
  const timestamp = new Date().toLocaleTimeString();
  return `[GitSocial Web] [${timestamp}] [${level.toUpperCase()}] ${message}`;
}

export function webLog(level: LogLevel, message: string, ...args: unknown[]): void {
  if (!shouldLog(level)) {
    return;
  }

  const formattedMessage = formatMessage(level, message);

  switch (level) {
  case 'error':
    console.error(formattedMessage, ...args);
    break;
  case 'warn':
    console.warn(formattedMessage, ...args);
    break;
  default:
    // eslint-disable-next-line no-console
    console.log(formattedMessage, ...args);
  }
}

export function setWebLogLevel(level: LogLevel): void {
  try {
    localStorage.setItem(STORAGE_KEY, level);
  } catch {
    // localStorage not available
  }
}

export function getWebLogLevel(): LogLevel {
  return getCurrentLevel();
}
