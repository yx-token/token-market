package listing

import (
	"errors"
	"testing"

	"github.com/yx-token/token-market/market/internal/platform/httpx"
)

func TestDispositionActiveErr(t *testing.T) {
	err := DispositionActiveErr(ModeBuyback)
	var he *httpx.Error
	if !errors.As(err, &he) {
		t.Fatalf("want httpx.Error, got %T", err)
	}
	if he.Status != 409 || he.Code != "DISPOSITION_ACTIVE" {
		t.Fatalf("status/code=%d/%s", he.Status, he.Code)
	}
	if he.Message == "" || !containsRune(he.Message, '回') {
		t.Fatalf("message should mention 回收: %q", he.Message)
	}
}

func containsRune(s string, r rune) bool {
	for _, c := range s {
		if c == r {
			return true
		}
	}
	return false
}
