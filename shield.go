package gocacheshield

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync/atomic"
	"time"

	"github.com/Zihad-Bepari/GoCacheShield/internal/bulkhead"
	"github.com/Zihad-Bepari/GoCacheShield/internal/coalescer"
	"github.com/Zihad-Bepari/GoCacheShield/internal/jitter"
	"github.com/Zihad-Bepari/GoCacheShield/internal/negcache"
)

// Engine is the central cache protection orchestrator.
// It manages the shared concurrency bulkhead, request coalescing, TTL jittering,
// and negative caching across multiple typed shields.
type Engine struct {
	opts             *options
	coalescer        *coalescer.Hub[[]byte]
	bulkhead         *bulkhead.Bulkhead
	fallbackBulkhead *bulkhead.Bulkhead
	jitter           *jitter.Calculator
	negCache         *negcache.Cache
	isClosed         atomic.Bool
}

// NewEngine creates and initializes a new GoCacheShield Engine.
func NewEngine(opts ...Option) *Engine {
	cfg := defaultOptions()
	for _, opt := range opts {
		opt(cfg)
	}

	var bhOpts []bulkhead.Option
	if cfg.maxWaitingLoads > 0 {
		bhOpts = append(bhOpts, bulkhead.WithQueue(cfg.maxWaitingLoads, cfg.waitTimeout))
	}

	return &Engine{
		opts:             cfg,
		coalescer:        coalescer.New[[]byte](),
		bulkhead:         bulkhead.New(cfg.maxConcurrentLoads, bhOpts...),
		fallbackBulkhead: bulkhead.New(cfg.fallbackMaxConcurrent),
		jitter:           jitter.New(cfg.jitterRatio, jitter.WithMinTTL(cfg.minTTL)),
		negCache:         negcache.New(cfg.negativeCacheCapacity),
	}
}

// GetOrLoadRaw executes or coalesces a loader for raw byte slices, protected by
// cache lookup, request coalescing, negative caching, and concurrency bulkheading.
func (e *Engine) GetOrLoadRaw(
	ctx context.Context,
	key string,
	ttl time.Duration,
	loader func(ctx context.Context) ([]byte, error),
) ([]byte, error) {
	if e.isClosed.Load() {
		return nil, ErrClosed
	}

	// check negative cache
	if e.negCache.IsNegative(key) {
		return nil, ErrNotFound
	}

	// check primary cache store
	cached, err := e.opts.store.Get(ctx, key)
	if err == nil {
		return cached, nil
	}

	var isBackendFailure bool
	if !errors.Is(err, ErrCacheMiss) {
		isBackendFailure = true
	}

	// coalesce concurrent calls
	raw, err := e.coalescer.Do(ctx, key, func(execCtx context.Context) ([]byte, error) {
		if e.negCache.IsNegative(key) {
			return nil, ErrNotFound
		}

		// secondary cache check after lock acquisition
		if !isBackendFailure {
			if doubleCheck, dcErr := e.opts.store.Get(execCtx, key); dcErr == nil {
				return doubleCheck, nil
			}
		}

		// acquire bulkhead slot
		activeBulkhead := e.bulkhead
		if isBackendFailure {
			activeBulkhead = e.fallbackBulkhead
		}

		release, bhErr := activeBulkhead.Acquire(execCtx)
		if bhErr != nil {
			if errors.Is(bhErr, bulkhead.ErrBulkheadSaturated) || errors.Is(bhErr, bulkhead.ErrQueueTimeout) {
				return nil, ErrTooManyLoads
			}
			if errors.Is(bhErr, bulkhead.ErrQueueFull) {
				return nil, ErrLoadQueueFull
			}
			return nil, bhErr
		}
		defer release()

		// execute loader
		val, loadErr := loader(execCtx)
		if loadErr != nil {
			if errors.Is(loadErr, ErrNotFound) {
				e.negCache.MarkNegative(key, e.opts.negativeTTL)
			}
			return nil, loadErr
		}

		// apply ttl jitter and store
		effectiveTTL := e.jitter.Apply(ttl)
		if setErr := e.opts.store.Set(execCtx, key, val, effectiveTTL); setErr != nil {
			e.negCache.Invalidate(key)
			return val, nil
		}

		e.negCache.Invalidate(key)
		return val, nil
	})

	if err != nil {
		if errors.Is(err, coalescer.ErrLoaderPanicked) {
			return nil, ErrLoaderPanicked
		}
		return nil, err
	}

	return raw, nil
}

// Invalidate removes the key from both the underlying cache store and negative cache.
func (e *Engine) Invalidate(ctx context.Context, key string) error {
	e.negCache.Invalidate(key)
	return e.opts.store.Delete(ctx, key)
}

// Close gracefully closes the engine and any closeable cache store.
func (e *Engine) Close() error {
	if e.isClosed.Swap(true) {
		return nil
	}
	if closer, ok := e.opts.store.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// BulkheadActive returns the number of active loader permits currently running.
func (e *Engine) BulkheadActive() int {
	return e.bulkhead.Active()
}

// TypedOption configures a TypedShield.
type TypedOption[T any] func(*TypedShield[T])

// WithCodec sets custom serialization and deserialization functions.
func WithCodec[T any](marshal func(T) ([]byte, error), unmarshal func([]byte) (T, error)) TypedOption[T] {
	return func(s *TypedShield[T]) {
		if marshal != nil && unmarshal != nil {
			s.marshal = marshal
			s.unmarshal = unmarshal
		}
	}
}

// TypedShield provides a strongly typed, generic API for caching entities.
type TypedShield[T any] struct {
	engine     *Engine
	defaultTTL time.Duration
	marshal    func(T) ([]byte, error)
	unmarshal  func([]byte) (T, error)
}

// NewTyped creates a new type-safe TypedShield instance tied to an Engine.
func NewTyped[T any](engine *Engine, defaultTTL time.Duration, opts ...TypedOption[T]) *TypedShield[T] {
	s := &TypedShield[T]{
		engine:     engine,
		defaultTTL: defaultTTL,
		marshal: func(val T) ([]byte, error) {
			return json.Marshal(val)
		},
		unmarshal: func(data []byte) (T, error) {
			var val T
			err := json.Unmarshal(data, &val)
			return val, err
		},
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// GetOrLoad fetches from cache or executes loader with defaultTTL.
func (s *TypedShield[T]) GetOrLoad(
	ctx context.Context,
	key string,
	loader func(ctx context.Context) (T, error),
) (T, error) {
	return s.GetOrLoadWithTTL(ctx, key, s.defaultTTL, loader)
}

// GetOrLoadWithTTL fetches from cache or executes loader with a custom TTL.
func (s *TypedShield[T]) GetOrLoadWithTTL(
	ctx context.Context,
	key string,
	ttl time.Duration,
	loader func(ctx context.Context) (T, error),
) (T, error) {
	raw, err := s.engine.GetOrLoadRaw(ctx, key, ttl, func(execCtx context.Context) ([]byte, error) {
		val, loadErr := loader(execCtx)
		if loadErr != nil {
			return nil, loadErr
		}
		return s.marshal(val)
	})

	if err != nil {
		var zero T
		return zero, err
	}

	return s.unmarshal(raw)
}

// Invalidate purges the item associated with key from the cache.
func (s *TypedShield[T]) Invalidate(ctx context.Context, key string) error {
	return s.engine.Invalidate(ctx, key)
}
