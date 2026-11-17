package broadcast

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestLockSerializesOneBroadcast(t *testing.T) {
	t.Parallel()
	var l Locks
	ctx := context.Background()
	counter, inside := 0, 0
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			unlock, err := l.Lock(ctx, "b1")
			if err != nil {
				t.Error(err)
				return
			}
			defer unlock()
			inside++ // the race detector flags a second holder
			if inside != 1 {
				t.Error("two holders at once")
			}
			counter++
			inside--
		})
	}
	wg.Wait()
	if counter != 50 {
		t.Fatalf("counter %d", counter)
	}
	if n := l.size(); n != 0 {
		t.Fatalf("%d entries left", n)
	}
}

func TestLockIndependentBroadcasts(t *testing.T) {
	t.Parallel()
	var l Locks
	ctx := context.Background()
	unlockA, err := l.Lock(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	defer unlockA()
	short, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	unlockB, err := l.Lock(short, "b") // must not wait for "a"
	if err != nil {
		t.Fatalf("b waited for a: %v", err)
	}
	unlockB()
}

func TestLockContextEnds(t *testing.T) {
	t.Parallel()
	var l Locks
	unlock, err := l.Lock(context.Background(), "b1")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := l.Lock(ctx, "b1"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want the deadline", err)
	}
	unlock()
	unlock() // a second call is harmless
	if n := l.size(); n != 0 {
		t.Fatalf("%d entries left", n)
	}
	unlock2, err := l.Lock(context.Background(), "b1")
	if err != nil {
		t.Fatal(err)
	}
	unlock2()
}
