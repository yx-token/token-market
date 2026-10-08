package ratelimit

import (
	"testing"
	"time"
)

func TestFailures(t *testing.T) {
	now := time.Unix(1000, 0)
	l := NewFailures(3, time.Minute)
	l.Now = func() time.Time { return now }
	for i := 0; i < 3; i++ {
		if l.Blocked("1.1.1.1") {
			t.Fatalf("blocked too early at %d", i)
		}
		l.Fail("1.1.1.1")
	}
	if !l.Blocked("1.1.1.1") || l.Blocked("2.2.2.2") {
		t.Fatal("limit should apply per key")
	}
	now = now.Add(time.Minute)
	if l.Blocked("1.1.1.1") {
		t.Fatal("window should reset")
	}
	l.Fail("x")
	l.Reset("x")
	if NewFailures(0, time.Minute).Blocked("x") {
		t.Fatal("limit 0 disables the limiter")
	}
}
