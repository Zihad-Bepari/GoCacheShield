package jitter

import (
	"math/rand"
	"sync"
	"testing"
	"time"
)

func TestJitter_ZeroRatio(t *testing.T) {
	calc := New(0)
	base := 5 * time.Minute
	if got := calc.Apply(base); got != base {
		t.Fatalf("expected exact base TTL %v, got %v", base, got)
	}
}

func TestJitter_Boundaries(t *testing.T) {
	ratio := 0.10 // +/- 10%
	calc := New(ratio)
	base := 60 * time.Second

	minExpected := 54 * time.Second
	maxExpected := 66 * time.Second

	for i := 0; i < 1000; i++ {
		got := calc.Apply(base)
		if got < minExpected || got > maxExpected {
			t.Fatalf("iteration %d: got %v, out of expected bounds [%v, %v]", i, got, minExpected, maxExpected)
		}
	}
}

func TestJitter_MinTTLFloor(t *testing.T) {
	calc := New(0.50, WithMinTTL(10*time.Second))
	base := 12 * time.Second // 12 - 50% = 6s, should be clamped to 10s

	for i := 0; i < 100; i++ {
		got := calc.Apply(base)
		if got < 10*time.Second {
			t.Fatalf("expected TTL >= 10s minTTL floor, got %v", got)
		}
	}
}

func TestJitter_DeterministicWithSeed(t *testing.T) {
	seed := int64(123456789)
	calc1 := New(0.20, WithRandSource(rand.NewSource(seed)))
	calc2 := New(0.20, WithRandSource(rand.NewSource(seed)))

	base := 10 * time.Minute
	for i := 0; i < 50; i++ {
		v1 := calc1.Apply(base)
		v2 := calc2.Apply(base)
		if v1 != v2 {
			t.Fatalf("expected deterministic output for same seed, got %v vs %v", v1, v2)
		}
	}
}

func TestJitter_ConcurrencySafe(t *testing.T) {
	calc := New(0.15)
	base := time.Minute
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = calc.Apply(base)
			}
		}()
	}
	wg.Wait()
}

func BenchmarkJitter_Apply(b *testing.B) {
	calc := New(0.15)
	base := 5 * time.Minute

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = calc.Apply(base)
		}
	})
}

