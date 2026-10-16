package gocacheshield

import "time"

// Option configures an Engine instance.
type Option func(*options)

type options struct {
	store                 CacheStore
	maxConcurrentLoads    int
	maxWaitingLoads       int
	waitTimeout           time.Duration
	jitterRatio           float64
	minTTL                time.Duration
	negativeTTL           time.Duration
	negativeCacheCapacity int
	fallbackMaxConcurrent int
}

func defaultOptions() *options {
	return &options{
		store:                 NewMemoryStore(time.Minute),
		maxConcurrentLoads:    100,
		maxWaitingLoads:       0,
		waitTimeout:           0,
		jitterRatio:           0.10, // 10% TTL jitter
		minTTL:                time.Second,
		negativeTTL:           30 * time.Second,
		negativeCacheCapacity: 10000,
		fallbackMaxConcurrent: 20,
	}
}

// WithCache configures the underlying CacheStore adapter (e.g. Redis, Memory, Memcached).
func WithCache(store CacheStore) Option {
	return func(o *options) {
		if store != nil {
			o.store = store
		}
	}
}

// WithMaxConcurrentLoads limits the number of simultaneous active loader executions.
// This forms the primary bulkhead protecting primary databases from distinct-key cache miss storms.
func WithMaxConcurrentLoads(n int) Option {
	return func(o *options) {
		if n > 0 {
			o.maxConcurrentLoads = n
		}
	}
}

// WithWaitQueue configures a bounded queue for loaders when the concurrent limit is saturated.
// If maxWaiters <= 0, excess requests are rejected immediately with ErrTooManyLoads.
func WithWaitQueue(maxWaiters int, timeout time.Duration) Option {
	return func(o *options) {
		if maxWaiters > 0 {
			o.maxWaitingLoads = maxWaiters
		}
		if timeout > 0 {
			o.waitTimeout = timeout
		}
	}
}

// WithJitter sets the jitter ratio applied to base TTLs to prevent cache avalanche.
// E.g., ratio = 0.15 applies +/- 15% random variance.
func WithJitter(ratio float64) Option {
	return func(o *options) {
		if ratio >= 0 {
			o.jitterRatio = ratio
		}
	}
}

// WithNegativeCaching configures negative caching for items returned with ErrNotFound.
// ttl specifies how long not-found results are cached.
// capacity specifies the maximum number of negative keys tracked in bounded LRU memory.
func WithNegativeCaching(ttl time.Duration, capacity int) Option {
	return func(o *options) {
		if ttl > 0 {
			o.negativeTTL = ttl
		}
		if capacity > 0 {
			o.negativeCacheCapacity = capacity
		}
	}
}

// WithFallbackLimit configures the degraded loader concurrency limit when the cache backend is down.
func WithFallbackLimit(n int) Option {
	return func(o *options) {
		if n > 0 {
			o.fallbackMaxConcurrent = n
		}
	}
}
