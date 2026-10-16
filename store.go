package gocacheshield

import (
	"context"
	"sync"
	"time"
)

// CacheStore represents the pluggable storage interface for GoCacheShield.
// It deals solely in raw bytes ([]byte), keeping serialization and reflection
// out of the critical caching path.
type CacheStore interface {
	// Get retrieves the raw cached bytes for key.
	// Must return ErrCacheMiss if the key does not exist or has expired.
	Get(ctx context.Context, key string) ([]byte, error)

	// Set stores the raw bytes for key with the specified TTL.
	// If ttl <= 0, the item should not expire (or adhere to backend defaults).
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error

	// Delete removes the item associated with key from the cache.
	Delete(ctx context.Context, key string) error
}

type memoryItem struct {
	data      []byte
	expiresAt time.Time
}

// MemoryStore is a zero-dependency, thread-safe in-memory cache store.
type MemoryStore struct {
	mu      sync.RWMutex
	items   map[string]memoryItem
	stopJan chan struct{}
}

// NewMemoryStore creates a new in-memory CacheStore.
// An optional cleanupInterval can be provided for periodic lazy expiration cleanup.
// If cleanupInterval <= 0, a default of 1 minute is used.
func NewMemoryStore(cleanupInterval time.Duration) *MemoryStore {
	if cleanupInterval <= 0 {
		cleanupInterval = time.Minute
	}
	s := &MemoryStore{
		items:   make(map[string]memoryItem),
		stopJan: make(chan struct{}),
	}
	go s.janitor(cleanupInterval)
	return s
}

// Get retrieves data from memory. If expired, it is purged and ErrCacheMiss is returned.
func (m *MemoryStore) Get(_ context.Context, key string) ([]byte, error) {
	m.mu.RLock()
	item, ok := m.items[key]
	m.mu.RUnlock()

	if !ok {
		return nil, ErrCacheMiss
	}

	if !item.expiresAt.IsZero() && time.Now().After(item.expiresAt) {
		m.mu.Lock()
		// Double check under write lock
		if it, exists := m.items[key]; exists && !it.expiresAt.IsZero() && time.Now().After(it.expiresAt) {
			delete(m.items, key)
		}
		m.mu.Unlock()
		return nil, ErrCacheMiss
	}

	// Return a copy so caller mutation does not corrupt stored data
	out := make([]byte, len(item.data))
	copy(out, item.data)
	return out, nil
}

// Set stores a defensive copy of value with an expiration deadline.
func (m *MemoryStore) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	dataCopy := make([]byte, len(value))
	copy(dataCopy, value)

	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = time.Now().Add(ttl)
	}

	m.mu.Lock()
	m.items[key] = memoryItem{
		data:      dataCopy,
		expiresAt: expiresAt,
	}
	m.mu.Unlock()
	return nil
}

// Delete removes an item from memory.
func (m *MemoryStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	delete(m.items, key)
	m.mu.Unlock()
	return nil
}

// Close stops the periodic expiration janitor.
func (m *MemoryStore) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	select {
	case <-m.stopJan:
		// already closed
	default:
		close(m.stopJan)
	}
	return nil
}

// Len returns the number of items currently stored in memory.
func (m *MemoryStore) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.items)
}

func (m *MemoryStore) janitor(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopJan:
			return
		case now := <-ticker.C:
			m.mu.Lock()
			for k, v := range m.items {
				if !v.expiresAt.IsZero() && now.After(v.expiresAt) {
					delete(m.items, k)
				}
			}
			m.mu.Unlock()
		}
	}
}
