package gocacheshield_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Zihad-Bepari/GoCacheShield"
	"github.com/Zihad-Bepari/GoCacheShield/internal/bulkhead"
	"github.com/Zihad-Bepari/GoCacheShield/internal/coalescer"
)

type BenchmarkEntity struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Score int    `json:"score"`
}

// BenchmarkShield_CacheHit measures the pure read overhead when data is cached.
func BenchmarkShield_CacheHit(b *testing.B) {
	engine := gocacheshield.NewEngine()
	defer engine.Close()

	shield := gocacheshield.NewTyped[BenchmarkEntity](engine, time.Hour)

	// Pre-warm the cache
	_, _ = shield.GetOrLoad(context.Background(), "bench:warm:key", func(ctx context.Context) (BenchmarkEntity, error) {
		return BenchmarkEntity{ID: "1", Title: "Benchmark", Score: 100}, nil
	})

	dummyLoader := func(ctx context.Context) (BenchmarkEntity, error) {
		return BenchmarkEntity{}, nil
	}

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		ctx := context.Background()
		for pb.Next() {
			val, err := shield.GetOrLoad(ctx, "bench:warm:key", dummyLoader)
			if err != nil || val.Score != 100 {
				b.Fatalf("cache hit failed: %v", err)
			}
		}
	})
}

// BenchmarkShield_NegativeCacheHit measures the instantaneous lookup for negative-cached items.
func BenchmarkShield_NegativeCacheHit(b *testing.B) {
	engine := gocacheshield.NewEngine(
		gocacheshield.WithNegativeCaching(time.Hour, 10000),
	)
	defer engine.Close()

	shield := gocacheshield.NewTyped[BenchmarkEntity](engine, time.Hour)

	// Mark key negative
	_, _ = shield.GetOrLoad(context.Background(), "bench:neg:key", func(ctx context.Context) (BenchmarkEntity, error) {
		return BenchmarkEntity{}, gocacheshield.ErrNotFound
	})

	dummyLoader := func(ctx context.Context) (BenchmarkEntity, error) {
		return BenchmarkEntity{}, nil
	}

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		ctx := context.Background()
		for pb.Next() {
			_, err := shield.GetOrLoad(ctx, "bench:neg:key", dummyLoader)
			if err == nil {
				b.Fatalf("expected ErrNotFound for negative cached key")
			}
		}
	})
}

// BenchmarkShield_CoalescingStampede measures throughput and loader call reduction under stampede.
func BenchmarkShield_CoalescingStampede(b *testing.B) {
	engine := gocacheshield.NewEngine(
		gocacheshield.WithMaxConcurrentLoads(100),
	)
	defer engine.Close()

	shield := gocacheshield.NewTyped[string](engine, 10*time.Millisecond)

	var loaderInvocations atomic.Uint64
	loader := func(ctx context.Context) (string, error) {
		loaderInvocations.Add(1)
		time.Sleep(500 * time.Microsecond) // simulate fast 0.5ms query
		return "payload", nil
	}

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		ctx := context.Background()
		for pb.Next() {
			_, _ = shield.GetOrLoad(ctx, "bench:stampede", loader)
		}
	})

	b.ReportMetric(float64(loaderInvocations.Load()), "loaders/total")
}

// BenchmarkBulkhead_AcquireRelease measures the raw overhead of permit acquisition and release.
func BenchmarkBulkhead_AcquireRelease(b *testing.B) {
	bh := bulkhead.New(1000)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			release, err := bh.Acquire(ctx)
			if err != nil {
				b.Fatal(err)
			}
			release()
		}
	})
}

// BenchmarkCoalescer_RawDo measures the microsecond overhead of the coalescing hub under contention.
func BenchmarkCoalescer_RawDo(b *testing.B) {
	hub := coalescer.New[string]()
	ctx := context.Background()

	fn := func(ctx context.Context) (string, error) {
		return "data", nil
	}

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = hub.Do(ctx, "bench:key", fn)
		}
	})
}
