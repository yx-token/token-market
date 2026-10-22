package buyer

import (
	"context"
	"errors"
	"testing"

	"github.com/yx-token/token-market/market/internal/platform/httpx"
)

func TestRefundPackDisabled(t *testing.T) {
	s := &KeyService{}
	_, err := s.RefundPack(context.Background(), 1, 2, "balance")
	var he *httpx.Error
	if !errors.As(err, &he) {
		t.Fatalf("want httpx.Error, got %T %v", err, err)
	}
	if he.Status != 403 || he.Code != "BUYER_REFUNDS_DISABLED" {
		t.Fatalf("status/code = %d/%s", he.Status, he.Code)
	}
}
