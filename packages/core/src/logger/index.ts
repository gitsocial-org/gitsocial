/**
 * Logger utility for GitSocial - Environment variable based logging
 */

// Define log levels with const assertion for better type safety
const LOG_LEVEL_VALUES = ['off', 'error', 'warn', 'info', 'debug', 'verbose'] as const;
export type LogLevel = typeof LOG_LEVEL_VALUES[number];

const LOG_LEVELS: Record<LogLevel, number> = {
  off: 0,
  error: 1,
  warn: 2,
  info: 3,
  debug: 4,
  verbose: 5
};

const LOG_PREFIX = '[GitSocial]';

function isValidLogLevel(level: string): boolean {
  return Object.keys(LOG_LEVELS).includes(level);
}

function getCurrentLogLevel(): LogLevel {
  if (typeof process !== 'undefined' && process.env) {
    const envLevel = process.env['GITSOCIAL_LOG_LEVEL'];
    if (envLevel && isValidLogLevel(envLevel)) {
      return envLevel as LogLevel;
    }
  }
  return 'off';
}

function shouldLog(level: LogLevel): boolean {
  const currentLevel = getCurrentLogLevel();
  return LOG_LEVELS[level] <= LOG_LEVELS[currentLevel];
}

function formatMessage(level: string, message: string, ...args: unknown[]): void {
  const timestamp = new Date().toISOString();
  const formattedMessage = `${LOG_PREFIX} [${timestamp}] [${level.toUpperCase()}] ${message}`;

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

export function getLogLevel(): LogLevel {
  return getCurrentLogLevel();
}

export function log(level: LogLevel, message: string, ...args: unknown[]): void {
  if (shouldLog(level)) {
    formatMessage(level, message, ...args);
  }
}
