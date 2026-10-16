package redis

import (
	"context"
	"errors"
	"time"

	"github.com/Zihad-Bepari/GoCacheShield"
)

// ErrNil is typically returned by Redis clients when a key does not exist (e.g. redis.Nil).
var ErrNil = errors.New("redis: nil")

// CmdRunner represents the minimal contract needed from a Redis client.
// Any client compatible with github.com/redis/go-redis/v9 can be adapted cleanly.
type CmdRunner interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, expiration time.Duration) error
	Del(ctx context.Context, keys ...string) error
}

// Store is a GoCacheShield CacheStore implementation backed by Redis.
type Store struct {
	client    CmdRunner
	keyPrefix string
}

// Option configures the Redis store.
type Option func(*Store)

// WithPrefix prepends a namespace prefix to all Redis keys (e.g., "app:cache:").
func WithPrefix(prefix string) Option {
	return func(s *Store) {
		s.keyPrefix = prefix
	}
}

// New creates a new Redis CacheStore adapter.
func New(client CmdRunner, opts ...Option) *Store {
	s := &Store{
		client: client,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Store) formatKey(key string) string {
	if s.keyPrefix == "" {
		return key
	}
	return s.keyPrefix + key
}

// Get retrieves raw bytes from Redis. Maps Redis nil/not-found to gocacheshield.ErrCacheMiss.
func (s *Store) Get(ctx context.Context, key string) ([]byte, error) {
	data, err := s.client.Get(ctx, s.formatKey(key))
	if err != nil {
		if errors.Is(err, ErrNil) || err.Error() == "redis: nil" {
			return nil, gocacheshield.ErrCacheMiss
		}
		return nil, err
	}
	return data, nil
}

// Set stores raw bytes in Redis with the given expiration.
func (s *Store) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return s.client.Set(ctx, s.formatKey(key), value, ttl)
}

// Delete removes the key from Redis.
func (s *Store) Delete(ctx context.Context, key string) error {
	return s.client.Del(ctx, s.formatKey(key))
}
