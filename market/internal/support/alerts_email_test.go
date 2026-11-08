package support

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type stubMailer struct {
	calls []struct{ to, subject, html string }
	err   error
}

func (s *stubMailer) Send(_ context.Context, to, subject, html string) error {
	s.calls = append(s.calls, struct{ to, subject, html string }{to, subject, html})
	return s.err
}

func TestBuildOverdueAlertHTML(t *testing.T) {
	html := buildOverdueAlertHTML([]overdueAlertRow{{
		ID: 7, UserEmail: "buyer@example.com", Waiting: 20 * time.Minute, Preview: "有人吗<script>",
	}}, 15, "https://app.example/admin/market/support")
	for _, want := range []string{
		"buyer@example.com", "#7", "20 分钟", "有人吗&lt;script&gt;",
		`href="https://app.example/admin/market/support"`, "超过",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %q in:\n%s", want, html)
		}
	}
}

func TestBuildBacklogAlertHTML(t *testing.T) {
	html := buildBacklogAlertHTML(6, 5, "")
	if !strings.Contains(html, "6") || !strings.Contains(html, "5") {
		t.Fatalf("unexpected html: %s", html)
	}
	if strings.Contains(html, "href=") {
		t.Fatal("empty workbench URL should omit link")
	}
}

func TestAlertConfigValidateAndEmails(t *testing.T) {
	cfg := DefaultAlertConfig()
	cfg.Emails = []string{" Ops@Example.com ", "ops@example.com", "bad"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected invalid email")
	}
	cfg.Emails = []string{"Ops@Example.com", "other@x.com"}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	got := cfg.NotifyEmails()
	if len(got) != 2 || got[0] != "ops@example.com" {
		t.Fatalf("got %#v", got)
	}
	cfg.EmailEnabled = false
	if len(cfg.NotifyEmails()) != 0 {
		t.Fatal("disabled should skip")
	}
}

func TestSendToAdminsPartialOK(t *testing.T) {
	m := &stubMailer{}
	n := 0
	m.err = nil
	// first succeeds via custom wrapper
	var failOnce = true
	sender := MailSenderFunc(func(ctx context.Context, to, subject, html string) error {
		n++
		if failOnce {
			failOnce = false
			return errors.New("boom")
		}
		return m.Send(ctx, to, subject, html)
	})
	if err := sendToAdmins(context.Background(), sender, []string{"a@x.com", "b@x.com"}, "subj", "body"); err != nil {
		t.Fatal(err)
	}
	if n != 2 || len(m.calls) != 1 {
		t.Fatalf("n=%d calls=%d", n, len(m.calls))
	}
}

func TestSendToAdminsAllFail(t *testing.T) {
	sender := MailSenderFunc(func(context.Context, string, string, string) error {
		return errors.New("smtp down")
	})
	err := sendToAdmins(context.Background(), sender, []string{"a@x.com"}, "s", "b")
	if err == nil || !strings.Contains(err.Error(), "smtp down") {
		t.Fatalf("got %v", err)
	}
}

// MailSenderFunc adapts a function to MailSender for tests.
type MailSenderFunc func(ctx context.Context, to, subject, html string) error

func (f MailSenderFunc) Send(ctx context.Context, to, subject, html string) error {
	return f(ctx, to, subject, html)
}
