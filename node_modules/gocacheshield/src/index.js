'use strict';

class CacheShieldError extends Error {
  constructor(message, code) {
    super(message);
    this.name = 'CacheShieldError';
    this.code = code;
  }
}

const ErrNotFound = new CacheShieldError('Item not found in loader', 'ERR_NOT_FOUND');
const ErrTooManyLoads = new CacheShieldError('Concurrent load limit exceeded', 'ERR_TOO_MANY_LOADS');
const ErrCacheMiss = new CacheShieldError('Key not found in cache', 'ERR_CACHE_MISS');
const ErrClosed = new CacheShieldError('Engine is closed', 'ERR_CLOSED');

class MemoryStore {
  constructor(capacity = 10000) {
    this.capacity = capacity;
    this.items = new Map();
  }

  async get(key) {
    const item = this.items.get(key);
    if (!item) return null;
    if (item.expiresAt && Date.now() > item.expiresAt) {
      this.items.delete(key);
      return null;
    }
    // Refresh access order for LRU
    this.items.delete(key);
    this.items.set(key, item);
    return item.value;
  }

  async set(key, value, ttlSeconds) {
    if (this.items.size >= this.capacity) {
      const oldestKey = this.items.keys().next().value;
      if (oldestKey) this.items.delete(oldestKey);
    }
    const expiresAt = ttlSeconds > 0 ? Date.now() + (ttlSeconds * 1000) : 0;
    this.items.set(key, { value, expiresAt });
  }

  async delete(key) {
    this.items.delete(key);
  }

  async clear() {
    this.items.clear();
  }
}

class CoalescerHub {
  constructor() {
    this.inFlight = new Map();
  }

  async do(key, fn) {
    const existing = this.inFlight.get(key);
    if (existing) {
      return existing; // Follower waits on in-flight promise
    }

    const promise = (async () => {
      try {
        return await fn();
      } finally {
        this.inFlight.delete(key);
      }
    })();

    this.inFlight.set(key, promise);
    return promise;
  }

  inFlightCount() {
    return this.inFlight.size;
  }
}

class Bulkhead {
  constructor(maxConcurrent = 50, maxQueue = 0, queueTimeoutMs = 1000) {
    this.maxConcurrent = maxConcurrent;
    this.maxQueue = maxQueue;
    this.queueTimeoutMs = queueTimeoutMs;
    this.active = 0;
    this.queue = [];
  }

  async acquire() {
    if (this.active < this.maxConcurrent) {
      this.active++;
      return () => this.release();
    }

    if (this.maxQueue <= 0 || this.queue.length >= this.maxQueue) {
      throw ErrTooManyLoads;
    }

    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        const idx = this.queue.findIndex(item => item.resolve === resolve);
        if (idx !== -1) {
          this.queue.splice(idx, 1);
        }
        reject(ErrTooManyLoads);
      }, this.queueTimeoutMs);

      this.queue.push({
        resolve: () => {
          clearTimeout(timer);
          resolve(() => this.release());
        },
        reject
      });
    });
  }

  release() {
    if (this.queue.length > 0) {
      const next = this.queue.shift();
      next.resolve();
    } else {
      this.active = Math.max(0, this.active - 1);
    }
  }

  activeCount() {
    return this.active;
  }
}

class JitterCalculator {
  constructor(ratio = 0.10, minTTL = 1) {
    this.ratio = Math.max(0, Math.min(1.0, ratio));
    this.minTTL = minTTL;
  }

  apply(baseTTL) {
    if (!baseTTL || baseTTL <= 0 || this.ratio === 0) return baseTTL;
    const factor = (Math.random() * 2.0) - 1.0; // [-1.0, 1.0]
    const delta = baseTTL * this.ratio * factor;
    return Math.max(this.minTTL, Math.round(baseTTL + delta));
  }
}

class NegativeCache {
  constructor(capacity = 5000) {
    this.capacity = capacity;
    this.items = new Map();
  }

  markNegative(key, ttlSeconds) {
    if (this.items.size >= this.capacity) {
      const oldestKey = this.items.keys().next().value;
      if (oldestKey) this.items.delete(oldestKey);
    }
    this.items.set(key, Date.now() + (ttlSeconds * 1000));
  }

  isNegative(key) {
    const expiresAt = this.items.get(key);
    if (!expiresAt) return false;
    if (Date.now() > expiresAt) {
      this.items.delete(key);
      return false;
    }
    return true;
  }

  invalidate(key) {
    this.items.delete(key);
  }
}

class CacheShield {
  constructor(options = {}) {
    this.store = options.store || new MemoryStore();
    this.coalescer = new CoalescerHub();
    this.bulkhead = new Bulkhead(
      options.maxConcurrentLoads || 50,
      options.maxQueue || 0,
      options.queueTimeoutMs || 1000
    );
    this.jitter = new JitterCalculator(options.jitterRatio ?? 0.10, options.minTTL || 1);
    this.negCache = new NegativeCache(options.negativeCapacity || 5000);
    this.negativeTTL = options.negativeTTL || 30; // seconds
    this.closed = false;
    this.stats = {
      totalCalls: 0,
      cacheHits: 0,
      coalesced: 0,
      savedQueries: 0,
      negativeHits: 0,
    };
  }

  async getOrLoad(key, ttlSeconds, loader) {
    if (this.closed) throw ErrClosed;
    this.stats.totalCalls++;

    // 1. Check Negative Cache
    if (this.negCache.isNegative(key)) {
      this.stats.negativeHits++;
      throw ErrNotFound;
    }

    // 2. Check Cache Store
    const cached = await this.store.get(key);
    if (cached !== null && cached !== undefined) {
      this.stats.cacheHits++;
      this.stats.savedQueries++;
      return cached;
    }

    // 3. Track Request Coalescing
    if (this.coalescer && this.coalescer.inFlight && this.coalescer.inFlight.has(key)) {
      this.stats.coalesced++;
      this.stats.savedQueries++;
    }

    // 4. Request Coalescing
    return this.coalescer.do(key, async () => {
      // Double check negative cache
      if (this.negCache.isNegative(key)) {
        this.stats.negativeHits++;
        throw ErrNotFound;
      }

      // Double check cache store
      const doubleCheck = await this.store.get(key);
      if (doubleCheck !== null && doubleCheck !== undefined) {
        this.stats.cacheHits++;
        this.stats.savedQueries++;
        return doubleCheck;
      }

      // 5. Acquire Bulkhead permit
      const release = await this.bulkhead.acquire();
      try {
        // 6. Execute user loader
        const val = await loader();

        // 7. Apply Jitter to TTL
        const effectiveTTL = this.jitter.apply(ttlSeconds);

        // 8. Store in Cache
        await this.store.set(key, val, effectiveTTL);
        this.negCache.invalidate(key);
        return val;
      } catch (err) {
        if (err === ErrNotFound || err?.code === 'ERR_NOT_FOUND') {
          this.stats.negativeHits++;
          this.negCache.markNegative(key, this.negativeTTL);
        }
        throw err;
      } finally {
        release();
      }
    });
  }

  getStats() {
    return {
      coalescedRequests: this.stats.coalesced,
      savedDBQueries: this.stats.savedQueries,
      cacheHits: this.stats.cacheHits,
      negativeCacheHits: this.stats.negativeHits,
      activeDBBulkhead: this.bulkhead ? this.bulkhead.activeCount() : 0,
      maxDBBulkhead: this.bulkhead ? this.bulkhead.maxConcurrent : 0,
      cacheStoreSize: this.store.items ? this.store.items.size : 0,
    };
  }

  async invalidate(key) {
    this.negCache.invalidate(key);
    await this.store.delete(key);
  }

  close() {
    this.closed = true;
    if (this.store.clear) {
      this.store.clear();
    }
  }
}

let _defaultCacheShield = null;

function getDefaultCacheShield() {
  if (!_defaultCacheShield) {
    _defaultCacheShield = new CacheShield({
      store: new MemoryStore(50000),
      maxConcurrentLoads: 200,
      maxQueue: 1000,
      queueTimeoutMs: 3000,
      jitterRatio: 0.15,
      negativeTTL: 60,
    });
  }
  return _defaultCacheShield;
}

/**
 * Universal GoCacheShield HTTP Middleware
 * Automatically protects ALL public GET routes in an Express application with:
 * 1. Fast In-Memory LRU Caching
 * 2. HTTP Request Coalescing (SingleFlight thundering herd merging)
 * 3. Negative Caching for 404s (blocks database penetration attacks)
 */
function cacheShieldMiddleware(options = {}) {
  const shield = options.shield || getDefaultCacheShield();
  const ttl = options.ttl || 60; // 60 seconds default TTL
  const pendingResponses = new Map();

  return async (req, res, next) => {
    // Only intercept public GET requests
    if (req.method !== 'GET') return next();

    const url = req.originalUrl || req.url;

    // Skip private, auth, admin or internal telemetry paths
    if (
      req.headers.authorization ||
      url.includes('/auth') ||
      url.includes('/loadguard') ||
      url.includes('/shield-metrics') ||
      url.includes('/telemetry') ||
      url.includes('/loadtest') ||
      url.includes('/health') ||
      url.includes('/uploads') ||
      (options.bypassPaths && options.bypassPaths.some((p) => url.includes(p)))
    ) {
      return next();
    }

    const key = `http_route:${url}`;

    // 1. Negative Cache check (Penetration Attack Prevention)
    if (shield.negCache && shield.negCache.isNegative(key)) {
      shield.stats.negativeHits++;
      res.setHeader('X-Cache-Shield', 'NEGATIVE-HIT');
      return res.status(404).json({
        success: false,
        message: 'Resource not found (Shielded)',
      });
    }

    // 2. Fast In-Memory Cache Check
    const cached = await shield.store.get(key);
    if (cached) {
      shield.stats.cacheHits++;
      shield.stats.savedQueries++;
      res.setHeader('X-Cache-Shield', 'HIT');
      res.setHeader('Content-Type', cached.contentType || 'application/json; charset=utf-8');
      return res.status(cached.status || 200).send(cached.body);
    }

    // 3. HTTP Request Coalescing (SingleFlight):
    // If another concurrent request is currently loading this exact same route from DB, wait and merge!
    if (pendingResponses.has(key)) {
      shield.stats.coalesced++;
      shield.stats.savedQueries++;
      try {
        const result = await pendingResponses.get(key);
        res.setHeader('X-Cache-Shield', 'COALESCED');
        res.setHeader('Content-Type', result.contentType || 'application/json; charset=utf-8');
        return res.status(result.status || 200).send(result.body);
      } catch (err) {
        return next(err);
      }
    }

    // 4. First caller: register deferred promise for coalescing
    let resolvePending, rejectPending;
    const pendingPromise = new Promise((resolve, reject) => {
      resolvePending = resolve;
      rejectPending = reject;
    });
    pendingResponses.set(key, pendingPromise);

    // Intercept response methods safely
    const originalSend = res.send ? res.send.bind(res) : null;
    let handled = false;

    if (originalSend) {
      res.send = function (body) {
        if (!handled) {
          handled = true;
          const statusCode = res.statusCode || 200;
          const contentType = (res.getHeader && res.getHeader('Content-Type')) || 'application/json; charset=utf-8';

          if (statusCode >= 200 && statusCode < 300) {
            shield.store.set(key, { status: statusCode, body, contentType }, ttl);
            resolvePending({ status: statusCode, body, contentType });
          } else if (statusCode === 404) {
            shield.stats.negativeHits++;
            shield.negCache.markNegative(key, shield.negativeTTL || 30);
            resolvePending({ status: statusCode, body, contentType });
          } else {
            resolvePending({ status: statusCode, body, contentType });
          }
          pendingResponses.delete(key);
        }
        return originalSend(body);
      };
    }

    const originalEnd = res.end ? res.end.bind(res) : null;
    if (originalEnd) {
      res.end = function (chunk, encoding) {
        if (!handled) {
          handled = true;
          pendingResponses.delete(key);
          resolvePending({
            status: res.statusCode || 200,
            body: chunk,
            contentType: res.getHeader ? res.getHeader('Content-Type') : 'application/json; charset=utf-8',
          });
        }
        return originalEnd(chunk, encoding);
      };
    }

    if (req.on) {
      req.on('error', (err) => {
        pendingResponses.delete(key);
        rejectPending(err);
      });
    }

    next();
  };
}

function createNestCacheShield(options = {}) {
  return new CacheShield(options);
}

module.exports = {
  CacheShield,
  MemoryStore,
  ErrNotFound,
  ErrTooManyLoads,
  ErrCacheMiss,
  ErrClosed,
  createNestCacheShield,
  get CacheShieldInterceptor() {
    return require('./nest').CacheShieldInterceptor;
  },
  cacheShieldMiddleware,
  getDefaultCacheShield,
};
