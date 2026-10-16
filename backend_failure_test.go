package gocacheshield_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gocacheshield/gocacheshield"
)

// FlakyStore simulates an external cache (e.g. Redis) that experiences network outages and errors.
type FlakyStore struct {
	mu         sync.RWMutex
	store      *gocacheshield.MemoryStore
	isDown     atomic.Bool
	readFails  atomic.Bool
	writeFails atomic.Bool
}

func NewFlakyStore() *FlakyStore {
	return &FlakyStore{
		store: gocacheshield.NewMemoryStore(time.Minute),
	}
}

var errSimulatedOutage = errors.New("simulated network connection timeout to redis")

func (f *FlakyStore) Get(ctx context.Context, key string) ([]byte, error) {
	if f.isDown.Load() || f.readFails.Load() {
		return nil, errSimulatedOutage
	}
	return f.store.Get(ctx, key)
}

func (f *FlakyStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if f.isDown.Load() || f.writeFails.Load() {
		return errSimulatedOutage
	}
	return f.store.Set(ctx, key, value, ttl)
}

func (f *FlakyStore) Delete(ctx context.Context, key string) error {
	if f.isDown.Load() {
		return errSimulatedOutage
	}
	return f.store.Delete(ctx, key)
}

func (f *FlakyStore) SetDown(down bool) {
	f.isDown.Store(down)
}

func (f *FlakyStore) SetWriteFail(fail bool) {
	f.writeFails.Store(fail)
}

// TestBackendFailure_WriteFailureTolerance verifies that if writing to cache fails,
// the loader's computation is still safely returned to the caller rather than failing the read.
func TestBackendFailure_WriteFailureTolerance(t *testing.T) {
	flaky := NewFlakyStore()
	flaky.SetWriteFail(true) // Cache reads work, but cache writes fail

	engine := gocacheshield.NewEngine(
		gocacheshield.WithCache(flaky),
	)
	defer engine.Close()

	shield := gocacheshield.NewTyped[string](engine, time.Minute)

	loader := func(ctx context.Context) (string, error) {
		return "loader-data", nil
	}

	val, err := shield.GetOrLoad(context.Background(), "user:profile", loader)
	if err != nil {
		t.Fatalf("read should succeed even when cache write fails, got err: %v", err)
	}
	if val != "loader-data" {
		t.Fatalf("expected 'loader-data', got: %s", val)
	}
}

// TestBackendFailure_OutageDegradedBulkhead verifies that during a complete cache outage,
// the fallback bulkhead prevents 100% of the traffic from overwhelming the primary database.
func TestBackendFailure_OutageDegradedBulkhead(t *testing.T) {
	flaky := NewFlakyStore()
	flaky.SetDown(true) // Redis goes completely down!

	const fallbackLimit = 5
	engine := gocacheshield.NewEngine(
		gocacheshield.WithCache(flaky),
		gocacheshield.WithFallbackLimit(fallbackLimit), // Strict bulkhead during outage
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

		time.Sleep(30 * time.Millisecond) // slow database query
		return "db-data", nil
	}

	// 50 requests arrive during the outage for distinct keys
	const callers = 50
	var wg sync.WaitGroup
	var successfulLoads atomic.Int32
	var throttledLoads atomic.Int32

	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			key := fmt.Sprintf("outage:key:%d", id)
			_, err := shield.GetOrLoad(context.Background(), key, loader)
			if err == nil {
				successfulLoads.Add(1)
			} else if errors.Is(err, gocacheshield.ErrTooManyLoads) {
				throttledLoads.Add(1)
			}
		}(i)
	}

	wg.Wait()

	if max := maxActiveObserved.Load(); max > fallbackLimit {
		t.Fatalf("fallback bulkhead breached during outage! observed %d, limit is %d", max, fallbackLimit)
	}

	if throttledLoads.Load() == 0 {
		t.Fatalf("expected degraded fallback to throttle excessive requests to DB during cache outage")
	}

	t.Logf("Outage handled: %d served within fallback budget, %d throttled", successfulLoads.Load(), throttledLoads.Load())
}

// TestBackendFailure_RecoveryAfterOutage verifies that when cache recovers,
// the shield automatically resumes normal cache population and high-throughput operation.
func TestBackendFailure_RecoveryAfterOutage(t *testing.T) {
	flaky := NewFlakyStore()
	flaky.SetDown(true) // Outage begins

	engine := gocacheshield.NewEngine(
		gocacheshield.WithCache(flaky),
		gocacheshield.WithFallbackLimit(10),
	)
	defer engine.Close()

	shield := gocacheshield.NewTyped[string](engine, time.Minute)

	var loaderCalls atomic.Int32
	loader := func(ctx context.Context) (string, error) {
		loaderCalls.Add(1)
		return "recovered-data", nil
	}

	// 1. Load during outage
	val, err := shield.GetOrLoad(context.Background(), "recov:key", loader)
	if err != nil || val != "recovered-data" {
		t.Fatalf("expected data during outage, got val=%s, err=%v", val, err)
	}

	// 2. Cache recovers (e.g. Redis reconnected)
	flaky.SetDown(false)

	// 3. Next call executes loader and successfully populates the cache
	val, err = shield.GetOrLoad(context.Background(), "recov:key", loader)
	if err != nil || val != "recovered-data" {
		t.Fatalf("expected data after recovery, got val=%s, err=%v", val, err)
	}

	callsAfterRepopulation := loaderCalls.Load()

	// 4. Subsequent call must be a CACHE HIT!
	val, err = shield.GetOrLoad(context.Background(), "recov:key", loader)
	if err != nil || val != "recovered-data" {
		t.Fatalf("expected cache hit after recovery, got val=%s, err=%v", val, err)
	}

	if loaderCalls.Load() != callsAfterRepopulation {
		t.Fatalf("cache hit failed after recovery! loader was called again")
	}
}
