# Changelog

All notable changes to **GoCacheShield** will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [v1.0.0] - 2026-10-14

### Added
- **Context-Aware Request Coalescing (`internal/coalescer`):**
  - Drop-in replacement for standard singleflight with full follower context cancellation safety.
  - Decoupled loader execution ensuring leader cancellations do not kill in-flight queries for remaining followers.
  - Built-in panic recovery trapping panics and propagating `ErrLoaderPanicked`.
- **Bounded Concurrency Bulkhead (`internal/bulkhead`):**
  - Semaphore-based concurrency limiter protecting primary databases against distinct-key cache miss storms.
  - Configurable waiting queue with timeout (`WithWaitQueue`).
- **Adaptive TTL Jitter (`internal/jitter`):**
  - Zero-allocation randomized TTL calculator preventing cache avalanche.
  - Mockable/deterministic testing support.
- **Bounded Negative Caching (`internal/negcache`):**
  - LRU-bounded negative cache protecting databases from cache penetration attacks.
  - Instantaneous (~68ns) lookup with zero allocations.
- **Cache Backend Failure Isolation:**
  - Automatic fallback bulkhead protecting databases during remote cache (e.g. Redis) outages.
  - Tolerant write policy ensuring loader results return cleanly even when cache writes fail.
  - Automatic recovery and cache repopulation upon backend restoration.
- **Pluggable Redis Adapter (`adapters/redis`):**
  - Compatible with `github.com/redis/go-redis/v9` without hard type-pinning.
  - Automatic mapping of `redis.Nil` to `ErrCacheMiss`.
  - Namespace prefixing support.
- **Pure Standard Library Core:**
  - Zero third-party dependencies in the core package.
