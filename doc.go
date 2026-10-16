// Package gocacheshield provides production-grade cache resilience and protection primitives for Go applications.
//
// Core Capabilities:
//   - Context-Aware Per-Key Request Coalescing (Singleflight replacement with follower cancellation safety)
//   - Bounded Concurrency Bulkhead (Protects databases from distinct-key cache miss storms)
//   - Adaptive TTL Jittering (Prevents cache avalanche)
//   - Bounded Negative Caching (Protects against cache penetration attacks)
//   - Cache Backend Failure Isolation (Fallback concurrency limiting during Redis/cache outages)
//   - Zero Unnecessary Dependencies (Pure standard library Go)
package gocacheshield
