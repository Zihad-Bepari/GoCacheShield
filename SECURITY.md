# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| v1.0.x  | :white_check_mark: |

## Threat Model & Mitigations

GoCacheShield has been engineered to mitigate several high-severity caching vulnerabilities:

1. **Denial of Service via Cache Stampede (Thundering Herd):**
   - *Threat:* Simultaneous expiration of a hot cache key causes thousands of parallel queries to primary databases, exhausting connection pools and causing system-wide outages.
   - *Mitigation:* Context-safe request coalescing collapses concurrent requests for the same missing key into a single backend fetch.

2. **Denial of Service via High-Cardinality Cache Misses:**
   - *Threat:* An attacker or spike in requests accesses thousands of distinct missing items simultaneously, completely bypassing standard singleflight.
   - *Mitigation:* The Bounded Concurrency Bulkhead strictly limits concurrent loader executions across all keys to `MaxConcurrentLoads`.

3. **Cache Penetration Attacks (Invalid/Non-Existent Keys):**
   - *Threat:* Attackers continuously query non-existent keys (e.g., `GET /user/random-uuid-404`) that never cache, forcing every request to hit the primary database.
   - *Mitigation:* Bounded negative caching caches `ErrNotFound` responses for a short duration with an LRU eviction bound, preventing memory exhaustion (OOM attacks).

4. **Cache Avalanche Outages:**
   - *Threat:* Massive numbers of keys set with identical fixed TTLs expire at the exact same second, causing sudden 0% hit ratios.
   - *Mitigation:* Adaptive TTL Jitter injects randomized variance into effective TTLs.

5. **Remote Cache Outage Cascades (e.g. Redis Outage):**
   - *Threat:* When Redis becomes unavailable, unchecked fallback code diverts 100% of read traffic to the primary database, immediately taking down the database.
   - *Mitigation:* Degraded fallback bulkhead restricts database throughput during cache outages, preserving system survival.

## Reporting a Vulnerability

If you discover a security vulnerability within GoCacheShield, please submit a report privately via GitHub Security Advisories or email the maintainers directly. Please do not open public GitHub issues for security vulnerabilities.
