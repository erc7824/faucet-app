package server

import (
	"sync"
	"time"
)

type rateLimiter struct {
	mu       sync.Mutex
	cooldown time.Duration
	seen     map[string]time.Time
}

func newRateLimiter(cooldown time.Duration) *rateLimiter {
	return &rateLimiter{
		cooldown: cooldown,
		seen:     make(map[string]time.Time),
	}
}

// checkAndRecord atomically checks if key is allowed and, if so, records the attempt.
// Returns true if the request is allowed (cooldown slot consumed), false if on cooldown.
func (r *rateLimiter) checkAndRecord(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	last, exists := r.seen[key]
	if exists && time.Since(last) < r.cooldown {
		return false
	}
	r.seen[key] = time.Now()
	return true
}
