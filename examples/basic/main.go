package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/Zihad-Bepari/GoCacheShield"
)

type Product struct {
	ID    string  `json:"id"`
	Title string  `json:"title"`
	Price float64 `json:"price"`
}

func main() {
	// 1. Initialize GoCacheShield Engine with in-memory store
	engine := gocacheshield.NewEngine(
		gocacheshield.WithMaxConcurrentLoads(20),                // Database bulkhead
		gocacheshield.WithJitter(0.15),                          // +/- 15% TTL jitter against avalanche
		gocacheshield.WithNegativeCaching(30*time.Second, 5000), // Protect against 404 penetration
	)
	defer engine.Close()

	// 2. Create strongly typed shield for Product entity
	productShield := gocacheshield.NewTyped[Product](engine, 5*time.Minute)

	// Simulated slow database loader
	loaderCount := 0
	productLoader := func(ctx context.Context) (Product, error) {
		loaderCount++
		fmt.Printf("--> [DATABASE] Querying product table for id: p101 (invocation #%d)...\n", loaderCount)
		time.Sleep(100 * time.Millisecond) // simulate slow query
		return Product{ID: "p101", Title: "Mechanical Keyboard", Price: 129.99}, nil
	}

	// 3. Simulate a concurrent Cache Stampede: 10 goroutines hit the same cold key simultaneously
	fmt.Println("=== Starting Concurrent Stampede Simulation (10 clients) ===")
	var wg sync.WaitGroup

	for i := 1; i <= 10; i++ {
		wg.Add(1)
		go func(clientID int) {
			defer wg.Done()
			p, err := productShield.GetOrLoad(context.Background(), "product:p101", productLoader)
			if err != nil {
				log.Printf("Client %d error: %v", clientID, err)
				return
			}
			fmt.Printf("Client %d received: %s ($%.2f)\n", clientID, p.Title, p.Price)
		}(i)
	}

	wg.Wait()
	fmt.Printf("=== Stampede Complete. Total Database Queries: %d (Expected: 1) ===\n\n", loaderCount)

	// 4. Subsequent read hits the cache immediately
	fmt.Println("=== Subsequent Read (Cache Hit) ===")
	cachedProduct, err := productShield.GetOrLoad(context.Background(), "product:p101", productLoader)
	if err != nil {
		log.Fatalf("unexpected error: %v", err)
	}
	fmt.Printf("Instant Cache Hit: %s ($%.2f), Database Queries still: %d\n\n", cachedProduct.Title, cachedProduct.Price, loaderCount)

	// 5. Test Negative Caching (Invalid entity)
	fmt.Println("=== Testing Negative Caching (Penetration Defense) ===")
	notFoundLoader := func(ctx context.Context) (Product, error) {
		fmt.Println("--> [DATABASE] Looking up invalid-product-999 (not found in DB)...")
		return Product{}, gocacheshield.ErrNotFound
	}

	_, err1 := productShield.GetOrLoad(context.Background(), "product:invalid-999", notFoundLoader)
	fmt.Printf("First call returned: %v (cached as negative)\n", err1)

	// Second call should return ErrNotFound immediately WITHOUT querying database
	_, err2 := productShield.GetOrLoad(context.Background(), "product:invalid-999", notFoundLoader)
	fmt.Printf("Second call returned: %v (served from negative cache, DB bypassed!)\n", err2)
}
