package broadcast

import (
	"context"
	"sync"
)

// Locks serializes everything that changes one broadcast (design D5): stage writes, go live,
// end, the WHIP requests, destination changes and the supervisor's events. One process runs
// per deployment (D12), so an in-process lock is enough. The zero value is ready to use.
type Locks struct {
	mu    sync.Mutex
	locks map[string]*entry
}

type entry struct {
	held  chan struct{} // holds a value while the lock is taken
	users int           // holders and waiters; the entry is dropped at zero
}

// Lock takes the lock of broadcast id, waiting until it is free or ctx ends. The returned
// function releases it and must be called exactly once.
func (l *Locks) Lock(ctx context.Context, id string) (unlock func(), err error) {
	l.mu.Lock()
	if l.locks == nil {
		l.locks = map[string]*entry{}
	}
	e := l.locks[id]
	if e == nil {
		e = &entry{held: make(chan struct{}, 1)}
		l.locks[id] = e
	}
	e.users++
	l.mu.Unlock()

	select {
	case e.held <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-e.held; l.release(id, e) }) }, nil
	case <-ctx.Done():
		l.release(id, e)
		return nil, ctx.Err()
	}
}

func (l *Locks) release(id string, e *entry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if e.users--; e.users == 0 {
		delete(l.locks, id)
	}
}

// size is the number of broadcasts with a holder or a waiter (tests).
func (l *Locks) size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.locks)
}
