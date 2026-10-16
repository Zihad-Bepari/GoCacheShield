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

type User struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func TestShield_CoalescingSingleFlight(t *testing.T) {
	engine := gocacheshield.NewEngine(
		gocacheshield.WithMaxConcurrentLoads(50),
	)
	defer engine.Close()

	shield := gocacheshield.NewTyped[User](engine, 5*time.Minute)

	var loaderCalls atomic.Int32
	loader := func(ctx context.Context) (User, error) {
		loaderCalls.Add(1)
		time.Sleep(50 * time.Millisecond) // simulate database query
		return User{ID: "u123", Name: "Zihad"}, nil
	}

	const callers = 50
	var wg sync.WaitGroup
	users := make([]User, callers)
	errs := make([]error, callers)

	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			u, err := shield.GetOrLoad(context.Background(), "user:123", loader)
			users[idx] = u
			errs[idx] = err
		}(i)
	}

	wg.Wait()

	if calls := loaderCalls.Load(); calls != 1 {
		t.Fatalf("expected exactly 1 loader invocation, got %d", calls)
	}

	for i, u := range users {
		if errs[i] != nil {
			t.Fatalf("caller %d got error: %v", i, errs[i])
		}
		if u.ID != "u123" || u.Name != "Zihad" {
			t.Fatalf("caller %d got unexpected user: %+v", i, u)
		}
	}
}

func TestShield_CacheHitBypassesLoader(t *testing.T) {
	engine := gocacheshield.NewEngine()
	defer engine.Close()

	shield := gocacheshield.NewTyped[User](engine, time.Minute)

	var loaderCalls atomic.Int32
	loader := func(ctx context.Context) (User, error) {
		loaderCalls.Add(1)
		return User{ID: "u1", Name: "Alice"}, nil
	}

	// 1st call: Cache Miss -> Loader called
	u1, err := shield.GetOrLoad(context.Background(), "user:1", loader)
	if err != nil || u1.Name != "Alice" {
		t.Fatalf("1st call failed: %v, %+v", err, u1)
	}
	if loaderCalls.Load() != 1 {
		t.Fatalf("expected 1 call, got %d", loaderCalls.Load())
	}

	// 2nd call: Cache Hit -> Loader must NOT be called
	u2, err := shield.GetOrLoad(context.Background(), "user:1", loader)
	if err != nil || u2.Name != "Alice" {
		t.Fatalf("2nd call failed: %v, %+v", err, u2)
	}
	if loaderCalls.Load() != 1 {
		t.Fatalf("expected cache hit to skip loader, calls: %d", loaderCalls.Load())
	}
}

func TestShield_BoundedConcurrency(t *testing.T) {
	// Bulkhead capacity: max 3 concurrent loads, queue capacity: 2
	engine := gocacheshield.NewEngine(
		gocacheshield.WithMaxConcurrentLoads(3),
		gocacheshield.WithWaitQueue(2, 200*time.Millisecond),
	)
	defer engine.Close()

	shield := gocacheshield.NewTyped[string](engine, time.Minute)

	var activeLoaders atomic.Int32
	var maxObservedLoaders atomic.Int32

	loader := func(ctx context.Context) (string, error) {
		current := activeLoaders.Add(1)
		defer activeLoaders.Add(-1)

		for {
			oldMax := maxObservedLoaders.Load()
			if current <= oldMax || maxObservedLoaders.CompareAndSwap(oldMax, current) {
				break
			}
		}

		time.Sleep(50 * time.Millisecond)
		return "done", nil
	}

	// Launch 10 distinct keys simultaneously
	var wg sync.WaitGroup
	var rejectedCount atomic.Int32

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			key := fmt.Sprintf("distinct-key-%d", id)
			_, err := shield.GetOrLoad(context.Background(), key, loader)
			if err != nil {
				if errors.Is(err, gocacheshield.ErrTooManyLoads) || errors.Is(err, gocacheshield.ErrLoadQueueFull) {
					rejectedCount.Add(1)
				}
			}
		}(i)
	}

	wg.Wait()

	if max := maxObservedLoaders.Load(); max > 3 {
		t.Fatalf("bulkhead breached! max concurrent loaders observed: %d, expected <= 3", max)
	}

	if rejectedCount.Load() == 0 {
		t.Fatalf("expected some requests to be rejected or timed out under burst")
	}
}

func TestShield_NegativeCachingPreventsPenetration(t *testing.T) {
	engine := gocacheshield.NewEngine(
		gocacheshield.WithNegativeCaching(200*time.Millisecond, 100),
	)
	defer engine.Close()

	shield := gocacheshield.NewTyped[User](engine, time.Minute)

	var loaderCalls atomic.Int32
	loader := func(ctx context.Context) (User, error) {
		loaderCalls.Add(1)
		return User{}, gocacheshield.ErrNotFound
	}

	// 1st request -> returns ErrNotFound, loader runs once
	_, err1 := shield.GetOrLoad(context.Background(), "user:invalid-404", loader)
	if !errors.Is(err1, gocacheshield.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err1)
	}
	if loaderCalls.Load() != 1 {
		t.Fatalf("expected 1 loader call, got %d", loaderCalls.Load())
	}

	// Next 5 requests within negative TTL -> must NOT invoke loader
	for i := 0; i < 5; i++ {
		_, err := shield.GetOrLoad(context.Background(), "user:invalid-404", loader)
		if !errors.Is(err, gocacheshield.ErrNotFound) {
			t.Fatalf("call %d: expected ErrNotFound, got %v", i, err)
		}
	}

	if loaderCalls.Load() != 1 {
		t.Fatalf("negative caching failed: loader was called %d times", loaderCalls.Load())
	}
}

func TestShield_Invalidate(t *testing.T) {
	engine := gocacheshield.NewEngine()
	defer engine.Close()

	shield := gocacheshield.NewTyped[string](engine, time.Minute)

	var version atomic.Int32
	loader := func(ctx context.Context) (string, error) {
		v := version.Add(1)
		return fmt.Sprintf("data-v%d", v), nil
	}

	res1, _ := shield.GetOrLoad(context.Background(), "cfg:version", loader)
	if res1 != "data-v1" {
		t.Fatalf("expected data-v1, got %s", res1)
	}

	// Invalidate cache
	if err := shield.Invalidate(context.Background(), "cfg:version"); err != nil {
		t.Fatalf("invalidate failed: %v", err)
	}

	// Should reload
	res2, _ := shield.GetOrLoad(context.Background(), "cfg:version", loader)
	if res2 != "data-v2" {
		t.Fatalf("expected data-v2 after invalidation, got %s", res2)
	}
}
