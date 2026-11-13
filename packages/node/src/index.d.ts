export interface CacheStore {
  get(key: string): Promise<any>;
  set(key: string, value: any, ttlSeconds: number): Promise<void>;
  delete(key: string): Promise<void>;
  clear?(): Promise<void>;
}

export interface CacheShieldOptions {
  store?: CacheStore;
  maxConcurrentLoads?: number;
  maxQueue?: number;
  queueTimeoutMs?: number;
  jitterRatio?: number;
  minTTL?: number;
  negativeCapacity?: number;
  negativeTTL?: number;
}

export declare const ErrNotFound: Error;
export declare const ErrTooManyLoads: Error;
export declare const ErrCacheMiss: Error;
export declare const ErrClosed: Error;

export declare class MemoryStore implements CacheStore {
  get(key: string): Promise<any>;
  set(key: string, value: any, ttlSeconds: number): Promise<void>;
  delete(key: string): Promise<void>;
  clear(): Promise<void>;
}

export declare class CacheShield {
  constructor(options?: CacheShieldOptions);
  getOrLoad<T>(key: string, ttlSeconds: number, loader: () => Promise<T>): Promise<T>;
  invalidate(key: string): Promise<void>;
  close(): void;
}

export declare function createNestCacheShield(options?: CacheShieldOptions): CacheShield;

export declare class CacheShieldInterceptor {
  constructor(shield?: CacheShield, options?: { ttl?: number; keyGenerator?: (ctx: any) => string });
  intercept(context: any, next: any): Promise<any>;
}
