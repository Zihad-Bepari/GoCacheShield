package negcache

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestNegativeCache_BasicAndExpiration(t *testing.T) {
	c := New(10)

	if c.IsNegative("key1") {
		t.Fatal("key1 should not be negative initially")
	}

	c.MarkNegative("key1", 50*time.Millisecond)

	if !c.IsNegative("key1") {
		t.Fatal("key1 should be negative immediately after marking")
	}

	time.Sleep(60 * time.Millisecond)

	if c.IsNegative("key1") {
		t.Fatal("key1 should have expired")
	}
	if c.Len() != 0 {
		t.Fatalf("expected lazy eviction to clean expired key, len is %d", c.Len())
	}
}

func TestNegativeCache_Invalidate(t *testing.T) {
	c := New(10)
	c.MarkNegative("user:404", time.Minute)

	if !c.IsNegative("user:404") {
		t.Fatal("expected user:404 to be negative")
	}

	// User is newly registered/created
	c.Invalidate("user:404")

	if c.IsNegative("user:404") {
		t.Fatal("user:404 should not be negative after invalidation")
	}
}

func TestNegativeCache_BoundedCapacityLRU(t *testing.T) {
	capacity := 3
	c := New(capacity)

	c.MarkNegative("k1", time.Minute)
	c.MarkNegative("k2", time.Minute)
	c.MarkNegative("k3", time.Minute)

	if c.Len() != 3 {
		t.Fatalf("expected len 3, got %d", c.Len())
	}

	// Access k1 to make it most recently used (k2 becomes oldest)
	_ = c.IsNegative("k1")

	// Add k4 -> should evict oldest (k2)
	c.MarkNegative("k4", time.Minute)

	if c.Len() != 3 {
		t.Fatalf("expected capacity bound of 3, got %d", c.Len())
	}

	if c.IsNegative("k2") {
		t.Fatal("k2 should have been evicted as the LRU element")
	}
	if !c.IsNegative("k1") {
		t.Fatal("k1 should still exist because it was refreshed")
	}
	if !c.IsNegative("k3") || !c.IsNegative("k4") {
		t.Fatal("k3 and k4 should both exist")
	}
}

func TestNegativeCache_ConcurrentSafe(t *testing.T) {
	c := New(50)
	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				key := fmt.Sprintf("k-%d-%d", id, j)
				c.MarkNegative(key, 100*time.Millisecond)
				_ = c.IsNegative(key)
			}
		}(i)
	}

	wg.Wait()
}

func BenchmarkNegativeCache_IsNegative(b *testing.B) {
	c := New(10000)
	c.MarkNegative("bench:neg", time.Hour)

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = c.IsNegative("bench:neg")
		}
	})
}

