package support

import (
	"testing"
	"time"
)

func TestNormalizeBody(t *testing.T) {
	if _, err := normalizeBody("  "); err == nil {
		t.Fatal("empty should fail")
	}
	got, err := normalizeBody("  hello  ")
	if err != nil || got != "hello" {
		t.Fatalf("got %q %v", got, err)
	}
}

func TestAnnotateOverdue(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	since := now.Add(-20 * time.Minute)
	c := Conversation{Status: StatusOpen, AwaitingStaffSince: &since}
	annotateOverdue(&c, now)
	if !c.Overdue || c.WaitingMinutes < 19 {
		t.Fatalf("expected overdue, got %+v", c)
	}
	fresh := now.Add(-5 * time.Minute)
	c2 := Conversation{Status: StatusOpen, AwaitingStaffSince: &fresh}
	annotateOverdue(&c2, now)
	if c2.Overdue {
		t.Fatal("5 minutes should not be overdue")
	}
}

func TestAnnotatePresenceTyping(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-2 * time.Second)
	stale := now.Add(-10 * time.Second)
	c := Conversation{userTypingAt: &fresh, staffTypingAt: &stale}
	annotatePresence(&c, now)
	annotateViewer(&c, SenderStaff)
	if !c.UserTyping || c.StaffTyping || !c.PeerTyping {
		t.Fatalf("want user typing + peer for staff, got %+v", c)
	}
	annotateViewer(&c, SenderUser)
	if c.PeerTyping {
		t.Fatal("staff typing expired; user peer should be false")
	}
}
