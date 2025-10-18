package api

import (
	"sync"
	"time"
)

// Throttler marks node URLs as throttled for a duration after 429 responses.
type Throttler struct {
	mu         sync.Mutex
	until      map[string]time.Time // url -> until
	backoffs   map[string]int       // url -> consecutive 429 count
	base       time.Duration
	maxBackoff time.Duration
}

func NewThrottler() *Throttler {
	return &Throttler{
		until:      make(map[string]time.Time),
		backoffs:   make(map[string]int),
		base:       30 * time.Second,
		maxBackoff: 10 * time.Minute,
	}
}

// IsThrottled returns true if url currently in throttle window.
func (t *Throttler) IsThrottled(url string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if u, ok := t.until[url]; ok {
		return time.Now().Before(u)
	}
	return false
}

// Mark429 registers a 429 for url and sets backoff.
// consecutive counts grow backoff exponentially (base * 2^(n-1)).
func (t *Throttler) Mark429(url string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.backoffs[url]++
	n := t.backoffs[url]
	backoff := t.base
	// exponential, cap by maxBackoff
	for i := 1; i < n; i++ {
		backoff = backoff * 2
		if backoff >= t.maxBackoff {
			backoff = t.maxBackoff
			break
		}
	}
	t.until[url] = time.Now().Add(backoff)
}

// Reset removes throttle/backoff for url (on success).
func (t *Throttler) Reset(url string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.until, url)
	delete(t.backoffs, url)
}
