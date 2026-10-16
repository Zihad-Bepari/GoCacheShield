package gocacheshield_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gocacheshield/gocacheshield"
)

// TestStress_HeavyConcurrentMissesOnSameKey tests 1,000 concurrent goroutines hitting the same cold key.
func TestStress_HeavyConcurrentMissesOnSameKey(t *testing.T) {
	engine := gocacheshield.NewEngine(
		gocacheshield.WithMaxConcurrentLoads(100),
	)
	defer engine.Close()

	shield := gocacheshield.NewTyped[string](engine, 5*time.Minute)

	var loaderInvocations atomic.Int32
	loader := func(ctx context.Context) (string, error) {
		loaderInvocations.Add(1)
		time.Sleep(20 * time.Millisecond) // slow database query
		return "hot-data", nil
	}

	const totalClients = 1000
	var wg sync.WaitGroup
	errCount := atomic.Int32{}

	startGate := make(chan struct{})

	for i := 0; i < totalClients; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-startGate // Synchronize stampede start

			val, err := shield.GetOrLoad(context.Background(), "stampede:key", loader)
			if err != nil {
				errCount.Add(1)
				return
			}
			if val != "hot-data" {
				errCount.Add(1)
			}
		}()
	}

	close(startGate) // Release stampede
	wg.Wait()

	if errs := errCount.Load(); errs != 0 {
		t.Fatalf("%d clients failed out of %d", errs, totalClients)
	}

	if calls := loaderInvocations.Load(); calls != 1 {
		t.Fatalf("expected exactly 1 loader invocation during stampede, got %d", calls)
	}
}

// TestStress_HighCardinalityKeysUnderBulkhead tests high volume of distinct keys under bounded concurrency.
func TestStress_HighCardinalityKeysUnderBulkhead(t *testing.T) {
	const maxConcurrent = 15
	const queueSize = 30

	engine := gocacheshield.NewEngine(
		gocacheshield.WithMaxConcurrentLoads(maxConcurrent),
		gocacheshield.WithWaitQueue(queueSize, 100*time.Millisecond),
	)
	defer engine.Close()

	shield := gocacheshield.NewTyped[string](engine, time.Minute)

	var activeLoaders atomic.Int32
	var maxActiveObserved atomic.Int32

	loader := func(ctx context.Context) (string, error) {
		current := activeLoaders.Add(1)
		defer activeLoaders.Add(-1)

		for {
			old := maxActiveObserved.Load()
			if current <= old || maxActiveObserved.CompareAndSwap(old, current) {
				break
			}
		}

		time.Sleep(10 * time.Millisecond)
		return "ok", nil
	}

	const distinctKeys = 200
	var wg sync.WaitGroup
	var successCount atomic.Int32
	var rejectedCount atomic.Int32

	for i := 0; i < distinctKeys; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			key := fmt.Sprintf("distinct:key:%d", id)
			_, err := shield.GetOrLoad(context.Background(), key, loader)
			if err == nil {
				successCount.Add(1)
			} else if errors.Is(err, gocacheshield.ErrTooManyLoads) || errors.Is(err, gocacheshield.ErrLoadQueueFull) {
				rejectedCount.Add(1)
			}
		}(i)
	}

	wg.Wait()

	if max := maxActiveObserved.Load(); max > maxConcurrent {
		t.Fatalf("bulkhead limit breached! max concurrent observed was %d, limit is %d", max, maxConcurrent)
	}

	totalHandled := successCount.Load() + rejectedCount.Load()
	if totalHandled != distinctKeys {
		t.Fatalf("all requests must be accounted for: handled=%d, total=%d", totalHandled, distinctKeys)
	}

	// Verify no permits leaked
	if active := engine.BulkheadActive(); active != 0 {
		t.Fatalf("expected 0 active permits after all requests completed, got %d", active)
	}
}

// TestStress_CancellationRace tests randomized context cancellations under high concurrency.
func TestStress_CancellationRace(t *testing.T) {
	engine := gocacheshield.NewEngine(
		gocacheshield.WithMaxConcurrentLoads(50),
	)
	defer engine.Close()

	shield := gocacheshield.NewTyped[string](engine, time.Minute)

	loader := func(ctx context.Context) (string, error) {
		time.Sleep(25 * time.Millisecond)
		return "survived", nil
	}

	const workers = 150
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			// Random cancel delay between 0 and 35ms
			cancelDelay := time.Duration(rand.Intn(35)) * time.Millisecond
			ctx, cancel := context.WithTimeout(context.Background(), cancelDelay)
			defer cancel()

			key := fmt.Sprintf("cancel:key:%d", id%5) // share across 5 keys
			val, err := shield.GetOrLoad(ctx, key, loader)
			if err == nil && val != "survived" {
				t.Errorf("unexpected value: %s", val)
			}
		}(i)
	}

	wg.Wait()
}

// TestStress_ConcurrentInvalidationAndReload verifies concurrent reads and invalidations.
func TestStress_ConcurrentInvalidationAndReload(t *testing.T) {
	engine := gocacheshield.NewEngine()
	defer engine.Close()

	shield := gocacheshield.NewTyped[int](engine, time.Minute)

	var counter atomic.Int32
	loader := func(ctx context.Context) (int, error) {
		return int(counter.Add(1)), nil
	}

	stop := make(chan struct{})
	go func() {
		time.Sleep(150 * time.Millisecond)
		close(stop)
	}()
	var wg sync.WaitGroup

	// Reader workers
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					key := fmt.Sprintf("inval:key:%d", id%3)
					val, err := shield.GetOrLoad(context.Background(), key, loader)
					if err != nil || val <= 0 {
						t.Errorf("read failed: val=%d, err=%v", val, err)
					}
					runtime.Gosched()
				}
			}
		}(i)
	}

	// Invalidator workers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					key := fmt.Sprintf("inval:key:%d", id%3)
					_ = shield.Invalidate(context.Background(), key)
					time.Sleep(5 * time.Millisecond)
				}
			}
		}(i)
	}

	wg.Wait()
}

// TestStress_EngineShutdownUnderLoad tests graceful engine closure while requests are in flight.
func TestStress_EngineShutdownUnderLoad(t *testing.T) {
	engine := gocacheshield.NewEngine(
		gocacheshield.WithMaxConcurrentLoads(10),
	)

	shield := gocacheshield.NewTyped[string](engine, time.Minute)

	started := make(chan struct{})
	loader := func(ctx context.Context) (string, error) {
		select {
		case <-started:
		default:
			close(started)
		}
		time.Sleep(50 * time.Millisecond)
		return "done", nil
	}

	// 1. Launch flight
	go func() {
		_, _ = shield.GetOrLoad(context.Background(), "shutdown:key", loader)
	}()

	<-started

	// 2. Close engine
	if err := engine.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	// 3. New requests after close must get ErrClosed
	_, err := shield.GetOrLoad(context.Background(), "shutdown:key2", loader)
	if !errors.Is(err, gocacheshield.ErrClosed) {
		t.Fatalf("expected ErrClosed on closed engine, got: %v", err)
	}
}

// TestStress_PanicRecoveryUnderBurst tests that multiple panicked loaders do not poison the system.
func TestStress_PanicRecoveryUnderBurst(t *testing.T) {
	engine := gocacheshield.NewEngine(
		gocacheshield.WithMaxConcurrentLoads(20),
	)
	defer engine.Close()

	shield := gocacheshield.NewTyped[string](engine, time.Minute)

	panickingLoader := func(ctx context.Context) (string, error) {
		panic("critical loader failure!")
	}

	const callers = 50
	var wg sync.WaitGroup
	var panickedCount atomic.Int32

	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			key := fmt.Sprintf("panic:key:%d", id%5) // 5 distinct panicking keys
			_, err := shield.GetOrLoad(context.Background(), key, panickingLoader)
			if errors.Is(err, gocacheshield.ErrLoaderPanicked) {
				panickedCount.Add(1)
			}
		}(i)
	}

	wg.Wait()

	if panickedCount.Load() != callers {
		t.Fatalf("expected all %d callers to receive ErrLoaderPanicked, got %d", callers, panickedCount.Load())
	}

	// After panics, system must recover and handle normal loaders cleanly
	goodLoader := func(ctx context.Context) (string, error) {
		return "recovered", nil
	}

	val, err := shield.GetOrLoad(context.Background(), "panic:key:0", goodLoader)
	if err != nil || val != "recovered" {
		t.Fatalf("system failed to recover after panic: val=%s, err=%v", val, err)
	}
}
