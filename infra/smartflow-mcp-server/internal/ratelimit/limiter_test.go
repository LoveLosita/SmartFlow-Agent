package ratelimit

import (
	"testing"
	"time"
)

func TestLimiter(t *testing.T) {
	l := New(2, 2)
	key := "user:tool"

	if !l.Allow(key) {
		t.Fatal("first request should pass")
	}
	if !l.Allow(key) {
		t.Fatal("second request should pass")
	}
	if l.Allow(key) {
		t.Fatal("third request should be rate limited")
	}

	time.Sleep(600 * time.Millisecond)
	if !l.Allow(key) {
		t.Fatal("request should pass after token refill")
	}
}
