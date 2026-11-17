package ratelimit

import (
	"fmt"
	"testing"
	"time"
)

func TestLimiter(t *testing.T) {
	clk := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	l := New(10, time.Minute, 3)
	l.now = func() time.Time { return clk }

	for i := 1; i <= 10; i++ {
		if !l.Allow("a") {
			t.Fatalf("attempt %d refused", i)
		}
	}
	if l.Allow("a") {
		t.Fatal("the eleventh attempt within a minute was allowed")
	}
	if !l.Allow("b") {
		t.Fatal("another key is limited too")
	}
	clk = clk.Add(6 * time.Second) // one token back
	if !l.Allow("a") || l.Allow("a") {
		t.Fatal("refill is not one token per 6 s")
	}
	clk = clk.Add(time.Hour)
	for i := 1; i <= 10; i++ {
		if !l.Allow("a") {
			t.Fatalf("after an hour, attempt %d refused", i)
		}
	}
	if l.Allow("a") {
		t.Fatal("the bucket holds more than the limit")
	}
}

func TestLimiterMemoryIsBounded(t *testing.T) {
	l := New(10, time.Minute, 100)
	for i := range 10_000 {
		l.Allow(fmt.Sprintf("10.0.%d.%d", i/256, i%256))
	}
	if n := len(l.keys); n != 100 || l.order.Len() != 100 {
		t.Fatalf("holds %d keys, want 100", n)
	}
}
