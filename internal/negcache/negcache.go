package negcache

import (
	"container/list"
	"sync"
	"time"
)

type entry struct {
	key       string
	expiresAt time.Time
}

// Cache provides a bounded, thread-safe LRU store for caching negative lookup results (ErrNotFound).
// This prevents cache penetration attacks while guaranteeing bounded memory consumption.
type Cache struct {
	mu       sync.Mutex
	capacity int
	evictList *list.List
	items    map[string]*list.Element
}

// New creates a bounded negative cache with the given capacity.
// If capacity <= 0, a default capacity of 10,000 entries is used.
func New(capacity int) *Cache {
	if capacity <= 0 {
		capacity = 10000
	}
	return &Cache{
		capacity:  capacity,
		evictList: list.New(),
		items:     make(map[string]*list.Element),
	}
}

// MarkNegative records a key as non-existent with the specified TTL.
func (c *Cache) MarkNegative(key string, ttl time.Duration) {
	if ttl <= 0 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	expiresAt := time.Now().Add(ttl)

	// If already in list, update expiration and move to front
	if el, found := c.items[key]; found {
		c.evictList.MoveToFront(el)
		el.Value.(*entry).expiresAt = expiresAt
		return
	}

	// Evict oldest if capacity is reached
	if c.evictList.Len() >= c.capacity {
		c.removeOldest()
	}

	ent := &entry{key: key, expiresAt: expiresAt}
	el := c.evictList.PushFront(ent)
	c.items[key] = el
}

// IsNegative checks whether the given key is currently marked as negative and not expired.
// If expired, it is lazily removed.
func (c *Cache) IsNegative(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	el, found := c.items[key]
	if !found {
		return false
	}

	ent := el.Value.(*entry)
	if time.Now().After(ent.expiresAt) {
		c.removeElement(el)
		return false
	}

	c.evictList.MoveToFront(el)
	return true
}

// Invalidate removes a key from the negative cache (e.g. when an item is newly created).
func (c *Cache) Invalidate(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if el, found := c.items[key]; found {
		c.removeElement(el)
	}
}

// Len returns the current number of tracked negative items.
func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.evictList.Len()
}

func (c *Cache) removeOldest() {
	el := c.evictList.Back()
	if el != nil {
		c.removeElement(el)
	}
}

func (c *Cache) removeElement(el *list.Element) {
	c.evictList.Remove(el)
	ent := el.Value.(*entry)
	delete(c.items, ent.key)
}
