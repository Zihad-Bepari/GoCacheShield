package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/Zihad-Bepari/GoCacheShield"
	"github.com/Zihad-Bepari/GoCacheShield/adapters/redis"
)

// SimpleMockRedis satisfies redis.CmdRunner for demonstration without external server.
type SimpleMockRedis struct {
	mu   sync.RWMutex
	data map[string][]byte
}

func (m *SimpleMockRedis) Get(_ context.Context, key string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	val, exists := m.data[key]
	if !exists {
		return nil, redis.ErrNil
	}
	return val, nil
}

func (m *SimpleMockRedis) Set(_ context.Context, key string, value []byte, _ time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = value
	return nil
}

func (m *SimpleMockRedis) Del(_ context.Context, keys ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range keys {
		delete(m.data, k)
	}
	return nil
}

type UserProfile struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}

func main() {
	// In production, instantiate your real go-redis client:
	// rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	mockRedis := &SimpleMockRedis{data: make(map[string][]byte)}

	// 1. Create Redis adapter with namespace prefix
	redisStore := redis.New(mockRedis, redis.WithPrefix("prod:app:"))

	// 2. Wrap with GoCacheShield Engine
	engine := gocacheshield.NewEngine(
		gocacheshield.WithCache(redisStore),
		gocacheshield.WithMaxConcurrentLoads(50),  // Normal database bulkhead
		gocacheshield.WithFallbackLimit(10),        // Degraded bulkhead during Redis outage!
		gocacheshield.WithJitter(0.10),             // 10% TTL jitter
	)
	defer engine.Close()

	// 3. Create typed shield
	userShield := gocacheshield.NewTyped[UserProfile](engine, 10*time.Minute)

	// Fetch or load
	user, err := userShield.GetOrLoad(context.Background(), "user:tausif", func(ctx context.Context) (UserProfile, error) {
		fmt.Println("--> Fetching user profile from primary PostgreSQL...")
		return UserProfile{Username: "tausif", Role: "admin"}, nil
	})
	if err != nil {
		log.Fatalf("failed: %v", err)
	}

	fmt.Printf("Loaded User: %s (%s)\n", user.Username, user.Role)

	// Invalidate when user changes role
	fmt.Println("Invalidating user cache...")
	if err := userShield.Invalidate(context.Background(), "user:tausif"); err != nil {
		log.Fatalf("invalidation failed: %v", err)
	}
	fmt.Println("User cache purged successfully.")
}
