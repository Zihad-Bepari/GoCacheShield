package bulkhead

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestBulkhead_ImmediateRejection(t *testing.T) {
	b := New(2) // capacity 2, no queue

	rel1, err := b.Acquire(context.Background())
	if err != nil {
		t.Fatalf("unexpected error on 1st acquire: %v", err)
	}
	defer rel1()

	rel2, err := b.Acquire(context.Background())
	if err != nil {
		t.Fatalf("unexpected error on 2nd acquire: %v", err)
	}
	defer rel2()

	// 3rd acquire must be rejected
	_, err = b.Acquire(context.Background())
	if !errors.Is(err, ErrBulkheadSaturated) {
		t.Fatalf("expected ErrBulkheadSaturated, got %v", err)
	}
	if b.Active() != 2 {
		t.Fatalf("expected 2 active, got %d", b.Active())
	}
	if b.TotalRejected() != 1 {
		t.Fatalf("expected 1 rejected, got %d", b.TotalRejected())
	}
}

func TestBulkhead_ReleaseAndReacquire(t *testing.T) {
	b := New(1)

	rel, err := b.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	// Release
	rel()
	// Idempotent test: double release must not over-fill permit channel
	rel()

	// Now should acquire cleanly
	rel2, err := b.Acquire(context.Background())
	if err != nil {
		t.Fatalf("re-acquire failed: %v", err)
	}
	defer rel2()

	if b.Active() != 1 {
		t.Fatalf("expected active 1, got %d", b.Active())
	}
}

func TestBulkhead_QueueWaitAndTimeout(t *testing.T) {
	b := New(1, WithQueue(1, 20*time.Millisecond))

	rel1, err := b.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire 1 failed: %v", err)
	}
	defer rel1()

	// 2nd acquire waits in queue and should time out
	start := time.Now()
	_, err = b.Acquire(context.Background())
	elapsed := time.Since(start)

	if !errors.Is(err, ErrQueueTimeout) {
		t.Fatalf("expected ErrQueueTimeout, got %v", err)
	}
	if elapsed < 15*time.Millisecond {
		t.Fatalf("expected timeout to wait at least ~20ms, elapsed: %v", elapsed)
	}
}

func TestBulkhead_QueueOverflow(t *testing.T) {
	b := New(1, WithQueue(1, 100*time.Millisecond))

	rel1, err := b.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire 1 failed: %v", err)
	}
	defer rel1()

	// Goroutine 1 enters queue
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = b.Acquire(context.Background())
	}()

	// Wait for goroutine 1 to be in queue
	time.Sleep(10 * time.Millisecond)

	// Next caller exceeds queue capacity of 1
	_, err = b.Acquire(context.Background())
	if !errors.Is(err, ErrQueueFull) {
		t.Fatalf("expected ErrQueueFull, got %v", err)
	}

	wg.Wait()
}

func TestBulkhead_ContextCancellation(t *testing.T) {
	b := New(1, WithQueue(5, 500*time.Millisecond))

	rel1, err := b.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire 1 failed: %v", err)
	}
	defer rel1()

	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	wg.Add(1)
	var cancelErr error
	go func() {
		defer wg.Done()
		_, cancelErr = b.Acquire(ctx)
	}()

	time.Sleep(10 * time.Millisecond)
	cancel() // cancel context while waiting in queue
	wg.Wait()

	if !errors.Is(cancelErr, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", cancelErr)
	}

	if b.Waiting() != 0 {
		t.Fatalf("expected 0 waiting callers, got %d", b.Waiting())
	}
}
