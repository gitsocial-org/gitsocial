export interface CacheEntry {
  filePath: string
  expires: number
}

export interface AvatarCacheStats {
  memoryCacheSize: number
  memoryCacheEntries: Array<{
    key: string
    expires: number
    isExpired: boolean
  }>
  fileStats?: {
    totalFiles: number
    diskUsage: number
    tempFiles: number
    permanentFiles: number
    directoryPath: string
    fileDetails: Array<{
      name: string
      size: number
      isTemp: boolean
    }>
  }
}

export interface ClearAvatarCacheOptions {
  clearMemoryCache?: boolean
  clearAllFiles?: boolean
  clearTempFiles?: boolean
}

export interface ClearResult {
  clearedMemoryEntries: number
  filesDeleted: number
  errors: string[]
}

export interface AvatarConfig {
  maxMemoryEntries: number
  cacheExpiration: number
  apiRateLimit: number
  apiTimeout: number
  tempFileCleanup: number
  userAgent: string
  defaultSizes: {
    api: number
    posts: number
    repository: number
  }
}

export const DEFAULT_AVATAR_CONFIG: AvatarConfig = {
  maxMemoryEntries: 500,
  cacheExpiration: 30 * 24 * 60 * 60 * 1000, // 30 days
  apiRateLimit: 100, // ms between API calls
  apiTimeout: 5000, // 5 seconds
  tempFileCleanup: 5 * 60 * 1000, // 5 minutes
  userAgent: 'VSCode-GitSocial/1.0',
  defaultSizes: {
    api: 80,
    posts: 40,
    repository: 16
  }
};
