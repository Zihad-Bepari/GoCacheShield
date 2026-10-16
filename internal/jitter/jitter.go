package jitter

import (
	"math/rand"
	"sync"
	"time"
)

// Calculator calculates randomized TTLs to prevent cache avalanche.
type Calculator struct {
	mu     sync.Mutex
	rng    *rand.Rand
	minTTL time.Duration
	ratio  float64
}

// Option configures a Calculator.
type Option func(*Calculator)

// WithMinTTL sets the minimum permitted TTL. Calculated TTL will never fall below this.
func WithMinTTL(min time.Duration) Option {
	return func(c *Calculator) {
		if min > 0 {
			c.minTTL = min
		}
	}
}

// WithRandSource provides a custom rand.Source for deterministic testing.
func WithRandSource(src rand.Source) Option {
	return func(c *Calculator) {
		if src != nil {
			c.rng = rand.New(src)
		}
	}
}

// New creates a new jitter Calculator.
// ratio defines the jitter percentage (e.g., 0.10 for +/- 10%).
// If ratio <= 0, no jitter is applied and base TTL is returned unmodified.
func New(ratio float64, opts ...Option) *Calculator {
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1.0 {
		ratio = 1.0
	}

	c := &Calculator{
		rng:    rand.New(rand.NewSource(time.Now().UnixNano())),
		ratio:  ratio,
		minTTL: time.Second, // default minimum TTL floor
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// Apply returns the baseTTL adjusted by a random jitter within [-ratio, +ratio].
// The result is always guaranteed to be >= minTTL (if baseTTL >= minTTL).
func (c *Calculator) Apply(baseTTL time.Duration) time.Duration {
	if baseTTL <= 0 || c.ratio == 0 {
		return baseTTL
	}

	c.mu.Lock()
	// random float in [-1.0, 1.0]
	factor := (c.rng.Float64() * 2.0) - 1.0
	c.mu.Unlock()

	delta := float64(baseTTL) * c.ratio * factor
	effective := time.Duration(float64(baseTTL) + delta)

	if effective < c.minTTL {
		effective = c.minTTL
	}

	return effective
}
