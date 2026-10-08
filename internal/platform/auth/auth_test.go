package auth

import (
	"strings"
	"testing"
	"time"
)

func TestPasswordRoundTrip(t *testing.T) {
	h, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(h, "correct horse battery") || CheckPassword(h, "wrong password") {
		t.Fatal("password check mismatch")
	}
	if _, err := HashPassword("short"); err == nil {
		t.Fatal("short password accepted")
	}
}

func TestTokenIssueAndParse(t *testing.T) {
	iss, err := NewIssuer(strings.Repeat("s", 32), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	tok, _, err := iss.Issue(Claims{UserID: 42, IsAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	c, err := iss.Parse(tok)
	if err != nil || c.UserID != 42 || !c.IsAdmin {
		t.Fatalf("parse: %v %+v", err, c)
	}

	other, _ := NewIssuer(strings.Repeat("x", 32), time.Hour)
	if _, err := other.Parse(tok); err == nil {
		t.Fatal("token signed by another secret accepted")
	}

	expired, _ := NewIssuer(strings.Repeat("s", 32), time.Hour)
	expired.now = func() time.Time { return time.Now().Add(-2 * time.Hour) }
	old, _, _ := expired.Issue(Claims{UserID: 1})
	if _, err := iss.Parse(old); err == nil {
		t.Fatal("expired token accepted")
	}
}
