package auth

import (
	"net/http"
	"sync"
	"time"
)

type tokenBucket struct {
	tokens   float64
	lastTime time.Time
	lastSeen time.Time
}

type KeyedRateLimiter struct {
	mu      sync.Mutex
	entries map[string]*tokenBucket
	rate    float64
	burst   float64
}

func NewKeyedRateLimiter(eventsPerSecond float64, burst int) *KeyedRateLimiter {
	return &KeyedRateLimiter{
		entries: make(map[string]*tokenBucket),
		rate:    eventsPerSecond,
		burst:   float64(burst),
	}
}

func (rl *KeyedRateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	if len(rl.entries) > 5000 {
		for k, e := range rl.entries {
			if now.Sub(e.lastSeen) > 15*time.Minute {
				delete(rl.entries, k)
			}
		}
	}

	b, ok := rl.entries[key]
	if !ok {
		b = &tokenBucket{
			tokens:   rl.burst,
			lastTime: now,
		}
		rl.entries[key] = b
	}

	elapsed := now.Sub(b.lastTime).Seconds()
	if elapsed > 0 {
		b.tokens += elapsed * rl.rate
		if b.tokens > rl.burst {
			b.tokens = rl.burst
		}
		b.lastTime = now
	}
	b.lastSeen = now

	if b.tokens < 1.0 {
		return false
	}
	b.tokens -= 1.0
	return true
}

func RateLimitMiddleware(rl *KeyedRateLimiter, keyFn func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := keyFn(r)
			if !rl.Allow(key) {
				w.Header().Set("Retry-After", "60")
				http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
