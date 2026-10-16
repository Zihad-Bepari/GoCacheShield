package gocacheshield

import "errors"

var (
	// ErrNotFound is returned by user loaders to indicate that the requested entity does not exist.
	// When returned, GoCacheShield can apply negative caching to protect against cache penetration.
	ErrNotFound = errors.New("gocacheshield: item not found in loader")

	// ErrTooManyLoads is returned when the concurrent loader bulkhead is saturated and cannot accept new loads.
	ErrTooManyLoads = errors.New("gocacheshield: concurrent load limit exceeded")

	// ErrLoadQueueFull is returned when the bulkhead's waiting queue is full.
	ErrLoadQueueFull = errors.New("gocacheshield: loader wait queue is full")

	// ErrCacheBackendDown is returned when the cache backend fails and degraded fallback policy rejects the request.
	ErrCacheBackendDown = errors.New("gocacheshield: cache backend is unavailable")

	// ErrLoaderPanicked is returned when the user-provided loader function panics during execution.
	ErrLoaderPanicked = errors.New("gocacheshield: loader panicked during execution")

	// ErrCacheMiss indicates that the requested key does not exist in the cache store.
	ErrCacheMiss = errors.New("gocacheshield: key not found in cache")

	// ErrNegativeCached is returned when an item is served from the negative cache (known not to exist).
	ErrNegativeCached = errors.New("gocacheshield: item is negative-cached")

	// ErrClosed is returned when operations are attempted on a closed shield or engine.
	ErrClosed = errors.New("gocacheshield: engine is closed")
)
