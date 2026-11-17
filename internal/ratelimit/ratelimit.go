// Package ratelimit is an in-memory token bucket per key (a client address), with a bounded
// table that evicts the least recently used key. Sign-in uses 10 per minute; the guest join
// reuses it with its own numbers.
package ratelimit

import (
	"container/list"
	"sync"
	"time"
)

// Limiter allows Limit events per Per for each key, keeping at most MaxKeys keys.
type Limiter struct {
	limit   float64
	per     time.Duration
	maxKeys int
	now     func() time.Time

	mu    sync.Mutex
	order *list.List // front: most recently used
	keys  map[string]*list.Element
}

type bucket struct {
	key    string
	tokens float64
	last   time.Time
}

// New returns a limiter of limit events per per for each key, remembering at most maxKeys keys.
func New(limit int, per time.Duration, maxKeys int) *Limiter {
	return &Limiter{
		limit: float64(limit), per: per, maxKeys: maxKeys, now: time.Now,
		order: list.New(), keys: map[string]*list.Element{},
	}
}

// Allow takes one token from key's bucket and reports whether there was one.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	el, ok := l.keys[key]
	if !ok {
		// an evicted key starts with a full bucket: the bound trades exactness for memory
		el = l.order.PushFront(&bucket{key: key, tokens: l.limit, last: now})
		l.keys[key] = el
		if l.order.Len() > l.maxKeys {
			oldest := l.order.Back()
			l.order.Remove(oldest)
			delete(l.keys, oldest.Value.(*bucket).key)
		}
	}
	l.order.MoveToFront(el)
	b := el.Value.(*bucket)
	b.tokens = min(l.limit, b.tokens+now.Sub(b.last).Seconds()*l.limit/l.per.Seconds())
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
