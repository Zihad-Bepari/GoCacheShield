package coalescer

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
)

// ErrLoaderPanicked is returned when the loader function panics.
var ErrLoaderPanicked = errors.New("coalescer: loader panicked during execution")

type flightCall[T any] struct {
	mu      sync.Mutex
	done    chan struct{}
	val     T
	err     error
	waiters atomic.Int64
}

// Hub provides context-safe, deduplicated execution of concurrent operations by key.
type Hub[T any] struct {
	mu    sync.Mutex
	calls map[string]*flightCall[T]
}

// New creates a new request coalescing Hub.
func New[T any]() *Hub[T] {
	return &Hub[T]{
		calls: make(map[string]*flightCall[T]),
	}
}

// Do executes or coalesces a loader call for the given key.
//
// Concurrency Guarantees:
// 1. Exactly one call to fn will be in flight for a given key.
// 2. Followers can cancel their wait independently via ctx without affecting the in-flight operation or other followers.
// 3. If the leader's ctx cancels, fn continues running to completion so other waiting callers receive the result.
// 4. If fn panics, the panic is recovered, all waiters receive ErrLoaderPanicked, and the internal map state is cleaned.
func (h *Hub[T]) Do(ctx context.Context, key string, fn func(ctx context.Context) (T, error)) (T, error) {
	h.mu.Lock()
	c, exists := h.calls[key]
	if exists {
		// We are a follower
		c.waiters.Add(1)
		h.mu.Unlock()
		return h.wait(ctx, key, c, false)
	}

	// We are the leader
	c = &flightCall[T]{
		done: make(chan struct{}),
	}
	c.waiters.Store(1)
	h.calls[key] = c
	h.mu.Unlock()

	// Launch execution
	go h.execute(key, c, fn)

	// Wait for completion or context cancellation
	return h.wait(ctx, key, c, true)
}

func (h *Hub[T]) execute(key string, c *flightCall[T], fn func(ctx context.Context) (T, error)) {
	defer func() {
		if r := recover(); r != nil {
			c.mu.Lock()
			c.err = ErrLoaderPanicked
			c.mu.Unlock()
		}

		h.mu.Lock()
		delete(h.calls, key)
		h.mu.Unlock()

		close(c.done)
	}()

	// Execute with a detached context so leader cancellation does not kill the shared loader
	val, err := fn(context.Background())

	c.mu.Lock()
	c.val = val
	c.err = err
	c.mu.Unlock()
}

func (h *Hub[T]) wait(ctx context.Context, key string, c *flightCall[T], isLeader bool) (T, error) {
	select {
	case <-ctx.Done():
		c.waiters.Add(-1)
		var zero T
		return zero, ctx.Err()
	case <-c.done:
		c.mu.Lock()
		val := c.val
		err := c.err
		c.mu.Unlock()
		return val, err
	}
}

// InFlightCount returns the number of unique keys currently being executed.
func (h *Hub[T]) InFlightCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.calls)
}
