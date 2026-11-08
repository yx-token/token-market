package mail

import (
	"context"
	"errors"
	"testing"
)

type seqSender struct {
	calls int
	err   error
}

func (s *seqSender) Send(context.Context, string, string, string) error {
	s.calls++
	return s.err
}

func TestFailoverSenderUsesFallbackOnSMTPAuth(t *testing.T) {
	primary := &seqSender{err: errors.New(`smtp auth: 535 "Authentication credentials invalid"`)}
	fallback := &seqSender{}
	f := FailoverSender{Primary: primary, Fallback: fallback}
	if err := f.Send(context.Background(), "a@b.com", "s", "h"); err != nil {
		t.Fatal(err)
	}
	if primary.calls != 1 || fallback.calls != 1 {
		t.Fatalf("primary=%d fallback=%d", primary.calls, fallback.calls)
	}
}

func TestFailoverSenderNoFallbackOnOtherError(t *testing.T) {
	primary := &seqSender{err: errors.New("network down")}
	fallback := &seqSender{}
	f := FailoverSender{Primary: primary, Fallback: fallback}
	if err := f.Send(context.Background(), "a@b.com", "s", "h"); err == nil {
		t.Fatal("expected error")
	}
	if fallback.calls != 0 {
		t.Fatal("fallback should not run")
	}
}
