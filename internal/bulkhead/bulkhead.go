package bulkhead

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

var (
	// ErrBulkheadSaturated is returned when all permits are busy and queue is disabled or full.
	ErrBulkheadSaturated = errors.New("bulkhead: concurrent limit exceeded")

	// ErrQueueFull is returned when the waiting queue has reached capacity.
	ErrQueueFull = errors.New("bulkhead: wait queue is full")

	// ErrQueueTimeout is returned when waiting for a permit exceeds the configured timeout.
	ErrQueueTimeout = errors.New("bulkhead: wait queue timeout")
)

// Bulkhead controls the maximum number of concurrent executions.
type Bulkhead struct {
	maxConcurrent int
	maxWaiters    int
	waitTimeout   time.Duration

	permits chan struct{}

	activeWaiters atomic.Int64
	totalAcquired atomic.Uint64
	totalRejected atomic.Uint64
}

// Option configures a Bulkhead instance.
type Option func(*Bulkhead)

// WithQueue enables a bounded waiting queue with a capacity and maximum wait duration.
func WithQueue(maxWaiters int, waitTimeout time.Duration) Option {
	return func(b *Bulkhead) {
		if maxWaiters > 0 {
			b.maxWaiters = maxWaiters
		}
		if waitTimeout > 0 {
			b.waitTimeout = waitTimeout
		}
	}
}

// New creates a new Bulkhead with the specified maxConcurrent capacity.
// If maxConcurrent <= 0, a default of 100 is used.
func New(maxConcurrent int, opts ...Option) *Bulkhead {
	if maxConcurrent <= 0 {
		maxConcurrent = 100
	}

	b := &Bulkhead{
		maxConcurrent: maxConcurrent,
		permits:       make(chan struct{}, maxConcurrent),
	}

	// Prime the permits channel
	for i := 0; i < maxConcurrent; i++ {
		b.permits <- struct{}{}
	}

	for _, opt := range opts {
		opt(b)
	}

	return b
}

// Acquire requests a permit to run a load operation.
// It returns an idempotent release callback on success, or an error if rejected or cancelled.
func (b *Bulkhead) Acquire(ctx context.Context) (func(), error) {
	// 1. Fast path: try non-blocking acquisition
	select {
	case <-b.permits:
		b.totalAcquired.Add(1)
		return b.makeReleaser(), nil
	default:
	}

	// 2. If no queue configured, reject immediately
	if b.maxWaiters <= 0 {
		b.totalRejected.Add(1)
		return nil, ErrBulkheadSaturated
	}

	// 3. Check queue capacity
	if int(b.activeWaiters.Load()) >= b.maxWaiters {
		b.totalRejected.Add(1)
		return nil, ErrQueueFull
	}

	// 4. Enqueue into wait pool
	b.activeWaiters.Add(1)
	defer b.activeWaiters.Add(-1)

	var timer *time.Timer
	var timeoutCh <-chan time.Time
	if b.waitTimeout > 0 {
		timer = time.NewTimer(b.waitTimeout)
		defer timer.Stop()
		timeoutCh = timer.C
	}

	select {
	case <-ctx.Done():
		b.totalRejected.Add(1)
		return nil, ctx.Err()
	case <-timeoutCh:
		b.totalRejected.Add(1)
		return nil, ErrQueueTimeout
	case <-b.permits:
		b.totalAcquired.Add(1)
		return b.makeReleaser(), nil
	}
}

func (b *Bulkhead) makeReleaser() func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			b.permits <- struct{}{}
		})
	}
}

// ActiveReturns the number of currently running executions holding a permit.
func (b *Bulkhead) Active() int {
	return b.maxConcurrent - len(b.permits)
}

// Capacity returns the maximum concurrent permits configured.
func (b *Bulkhead) Capacity() int {
	return b.maxConcurrent
}

// Waiting returns the number of callers currently queued waiting for a permit.
func (b *Bulkhead) Waiting() int {
	return int(b.activeWaiters.Load())
}

// TotalAcquired returns the cumulative count of successfully acquired permits.
func (b *Bulkhead) TotalAcquired() uint64 {
	return b.totalAcquired.Load()
}

// TotalRejected returns the cumulative count of rejected permit acquisitions.
func (b *Bulkhead) TotalRejected() uint64 {
	return b.totalRejected.Load()
}
