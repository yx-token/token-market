package account

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

type captureMailer struct {
	to, subject, html string
	err               error
}

func (c *captureMailer) Send(_ context.Context, to, subject, html string) error {
	c.to, c.subject, c.html = to, subject, html
	return c.err
}

func TestSendAndConsumeRegisterCode(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	mailer := &captureMailer{}
	s := &Service{}
	s.ConfigureEmailFixed(rdb, mailer, true)

	email := "user@example.com"
	code := "123456"
	if err := rdb.Set(context.Background(), registerCodeKeyPrefix+email, code, registerCodeTTL).Err(); err != nil {
		t.Fatal(err)
	}
	if err := s.consumeRegisterCode(context.Background(), email, "000000"); err != ErrInvalidCode {
		t.Fatalf("want INVALID_CODE, got %v", err)
	}
	if err := s.consumeRegisterCode(context.Background(), email, code); err != nil {
		t.Fatal(err)
	}
	if err := s.consumeRegisterCode(context.Background(), email, code); err != ErrInvalidCode {
		t.Fatalf("code should be single-use, got %v", err)
	}
}

func TestConsumeSkippedWhenDisabled(t *testing.T) {
	s := &Service{}
	if err := s.consumeRegisterCode(context.Background(), "a@b.c", ""); err != nil {
		t.Fatal(err)
	}
}

func TestRandomDigits(t *testing.T) {
	got, err := randomDigits(6)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 6 {
		t.Fatalf("len=%d", len(got))
	}
	for _, c := range got {
		if c < '0' || c > '9' {
			t.Fatalf("non-digit %q", got)
		}
	}
}
