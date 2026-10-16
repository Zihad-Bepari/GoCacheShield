package redis_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gocacheshield/gocacheshield"
	"github.com/gocacheshield/gocacheshield/adapters/redis"
)

type mockRedisClient struct {
	data       map[string][]byte
	injectedErr error
}

func newMockRedisClient() *mockRedisClient {
	return &mockRedisClient{
		data: make(map[string][]byte),
	}
}

func (m *mockRedisClient) Get(_ context.Context, key string) ([]byte, error) {
	if m.injectedErr != nil {
		return nil, m.injectedErr
	}
	val, exists := m.data[key]
	if !exists {
		return nil, redis.ErrNil
	}
	return val, nil
}

func (m *mockRedisClient) Set(_ context.Context, key string, value []byte, _ time.Duration) error {
	if m.injectedErr != nil {
		return m.injectedErr
	}
	m.data[key] = value
	return nil
}

func (m *mockRedisClient) Del(_ context.Context, keys ...string) error {
	if m.injectedErr != nil {
		return m.injectedErr
	}
	for _, k := range keys {
		delete(m.data, k)
	}
	return nil
}

func TestRedisAdapter_GetHitAndMiss(t *testing.T) {
	mock := newMockRedisClient()
	adapter := redis.New(mock, redis.WithPrefix("test:"))

	ctx := context.Background()

	// 1. Miss returns ErrCacheMiss
	_, err := adapter.Get(ctx, "k1")
	if !errors.Is(err, gocacheshield.ErrCacheMiss) {
		t.Fatalf("expected ErrCacheMiss, got %v", err)
	}

	// 2. Set with prefix
	if err := adapter.Set(ctx, "k1", []byte("val1"), time.Minute); err != nil {
		t.Fatalf("set failed: %v", err)
	}

	// Verify key has prefix in underlying client
	if _, exists := mock.data["test:k1"]; !exists {
		t.Fatalf("expected underlying key to have prefix 'test:k1'")
	}

	// 3. Get Hit
	got, err := adapter.Get(ctx, "k1")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if string(got) != "val1" {
		t.Fatalf("expected 'val1', got '%s'", string(got))
	}

	// 4. Delete
	if err := adapter.Delete(ctx, "k1"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	_, err = adapter.Get(ctx, "k1")
	if !errors.Is(err, gocacheshield.ErrCacheMiss) {
		t.Fatalf("expected ErrCacheMiss after delete, got %v", err)
	}
}

func TestRedisAdapter_IntegrationWithShield(t *testing.T) {
	mock := newMockRedisClient()
	adapter := redis.New(mock, redis.WithPrefix("prod:"))

	engine := gocacheshield.NewEngine(
		gocacheshield.WithCache(adapter),
	)
	defer engine.Close()

	shield := gocacheshield.NewTyped[string](engine, time.Minute)

	callCount := 0
	loader := func(ctx context.Context) (string, error) {
		callCount++
		return "redis-value", nil
	}

	// 1st call -> loader
	v1, err := shield.GetOrLoad(context.Background(), "item:100", loader)
	if err != nil || v1 != "redis-value" {
		t.Fatalf("1st call failed: %v, %s", err, v1)
	}
	if callCount != 1 {
		t.Fatalf("expected 1 call, got %d", callCount)
	}

	// 2nd call -> served from Redis adapter cache
	v2, err := shield.GetOrLoad(context.Background(), "item:100", loader)
	if err != nil || v2 != "redis-value" {
		t.Fatalf("2nd call failed: %v, %s", err, v2)
	}
	if callCount != 1 {
		t.Fatalf("expected cache hit from Redis adapter, got %d calls", callCount)
	}
}
