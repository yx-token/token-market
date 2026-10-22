package billing

import (
	"errors"
	"testing"

	"github.com/yx-token/token-market/market/internal/platform/httpx"
)

func TestDenyBuyerRefund(t *testing.T) {
	err := DenyBuyerRefund()
	var he *httpx.Error
	if !errors.As(err, &he) {
		t.Fatalf("want httpx.Error, got %T %v", err, err)
	}
	if he.Status != 403 || he.Code != "BUYER_REFUNDS_DISABLED" {
		t.Fatalf("status/code = %d/%s", he.Status, he.Code)
	}
}
