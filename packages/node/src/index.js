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
  constructor() {
    this.items = new Map();
  }

  async get(key) {
    const item = this.items.get(key);
    if (!item) return null;
    if (item.expiresAt && Date.now() > item.expiresAt) {
      this.items.delete(key);
      return null;
    }
    return item.value;
  }

  async set(key, value, ttlSeconds) {
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
  }

  async getOrLoad(key, ttlSeconds, loader) {
    if (this.closed) throw ErrClosed;

    // 1. Check Negative Cache
    if (this.negCache.isNegative(key)) {
      throw ErrNotFound;
    }

    // 2. Check Cache Store
    const cached = await this.store.get(key);
    if (cached !== null && cached !== undefined) {
      return cached;
    }

    // 3. Request Coalescing
    return this.coalescer.do(key, async () => {
      // Double check negative cache
      if (this.negCache.isNegative(key)) {
        throw ErrNotFound;
      }

      // Double check cache store
      const doubleCheck = await this.store.get(key);
      if (doubleCheck !== null && doubleCheck !== undefined) {
        return doubleCheck;
      }

      // 4. Acquire Bulkhead permit
      const release = await this.bulkhead.acquire();
      try {
        // 5. Execute user loader
        const val = await loader();

        // 6. Apply Jitter to TTL
        const effectiveTTL = this.jitter.apply(ttlSeconds);

        // 7. Store in Cache
        await this.store.set(key, val, effectiveTTL);
        this.negCache.invalidate(key);
        return val;
      } catch (err) {
        if (err === ErrNotFound || err?.code === 'ERR_NOT_FOUND') {
          this.negCache.markNegative(key, this.negativeTTL);
        }
        throw err;
      } finally {
        release();
      }
    });
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

module.exports = {
  CacheShield,
  MemoryStore,
  ErrNotFound,
  ErrTooManyLoads,
  ErrCacheMiss,
  ErrClosed
};
