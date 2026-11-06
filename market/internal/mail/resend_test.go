package mail

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResendSend(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer re_test" {
			t.Fatalf("auth=%q", r.Header.Get("Authorization"))
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"to":["a@b.c"]`) {
			t.Fatalf("body=%s", body)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"ok"}`))
	}))
	t.Cleanup(srv.Close)

	r := NewResend("re_test", "闲猫 <noreply@idlicat.com>")
	r.APIURL = srv.URL
	r.Client = srv.Client()
	if err := r.Send(context.Background(), "a@b.c", "subj", "<b>hi</b>"); err != nil {
		t.Fatal(err)
	}
}

func TestResendNotConfigured(t *testing.T) {
	if err := (&Resend{}).Send(context.Background(), "a@b.c", "s", "h"); err != ErrNotConfigured {
		t.Fatalf("got %v", err)
	}
}
