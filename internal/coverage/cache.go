package coverage

import (
	"sync"
	"time"
)

// cacheTTL is how long a built report (and the all-time last-heard lookup)
// is served before it is rebuilt.
const cacheTTL = 5 * time.Minute

// ttlCache is a small mutex-guarded map with per-entry expiry. Entries are
// few (one per parameter set the admin UI asks for), so expired ones are
// swept on insert rather than by a goroutine.
type ttlCache[K comparable, V any] struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[K]ttlEntry[V]
}

type ttlEntry[V any] struct {
	value   V
	expires time.Time
}

func newTTLCache[K comparable, V any](ttl time.Duration) *ttlCache[K, V] {
	return &ttlCache[K, V]{ttl: ttl, entries: map[K]ttlEntry[V]{}}
}

func (c *ttlCache[K, V]) get(k K, now time.Time) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[k]
	if !ok || !now.Before(e.expires) {
		var zero V
		return zero, false
	}
	return e.value, true
}

func (c *ttlCache[K, V]) put(k K, v V, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, e := range c.entries {
		if !now.Before(e.expires) {
			delete(c.entries, key)
		}
	}
	c.entries[k] = ttlEntry[V]{value: v, expires: now.Add(c.ttl)}
}

func (c *ttlCache[K, V]) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.entries)
}
