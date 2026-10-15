//go:build integration

package e2e

import (
	"fmt"
	"math"
	"testing"

	"github.com/DueGin/token-market/internal/billing"
	"github.com/DueGin/token-market/internal/listing"
)

// 通过真实 HTTP、PostgreSQL 与替身上游验收购买快照和资金隔离，
// 不以卖家表单字段或前端显示作为资金生效的证据。
func TestCreditPackPurchaseIsolationAndSnapshot(t *testing.T) {
	e := setup(t)
	admin := e.login("admin@market.test", "admin-password-1")
	for _, platform := range []string{"openai", "anthropic"} {
		admin.must("PUT", "/api/admin/vendors/"+platform, map[string]bool{"enabled": true}, nil)
	}
	admin.must("PUT", "/api/admin/settings", map[string]any{"credit_usd_to_balance_rate": 2}, nil)
	seller := e.newUser("credit-accept-seller@market.test")
	other := e.newUser("credit-accept-other@market.test")
	buyer := e.newUser("credit-accept-buyer@market.test")

	var offer listing.CreditOffer
	seller.must("POST", "/api/seller/credit-offers", map[string]any{
		"title": "八十元买一百美元", "price_cny": 80, "face_value_usd": 100,
		"allowed_platforms": []string{"openai"}, "allowed_models": []string{"gpt-6-astra"},
	}, &offer)
	var recharge struct {
		Order idOnly `json:"order"`
	}
	buyer.must("POST", "/api/buyer/recharge", map[string]any{"amount": 100}, &recharge)
	admin.must("POST", fmt.Sprintf("/api/admin/recharges/%d/confirm", recharge.Order.ID), map[string]string{"provider_ref": "credit-accept-funds"}, nil)
	e.drain()
	var pack, replay billing.CreditPack
	request := map[string]any{"offer_id": offer.ID, "idempotency_key": "acceptance-purchase-1"}
	buyer.must("POST", "/api/buyer/credit-packs", request, &pack)
	buyer.must("POST", "/api/buyer/credit-packs", request, &replay)
	if pack.ID != replay.ID || pack.SellerID != offer.SellerID || pack.FaceValueUSD != 100 || pack.PriceCNY != 80 || pack.USDToBalanceRate != 2 {
		t.Fatalf("purchase snapshot or idempotency mismatch: first=%+v replay=%+v", pack, replay)
	}
	other.expect(404, "POST", fmt.Sprintf("/api/buyer/credit-packs/%d/refund", pack.ID), nil)
	other.expect(404, "PUT", fmt.Sprintf("/api/seller/credit-offers/%d", offer.ID), map[string]any{
		"title": "不属于此卖家", "price_cny": 1, "face_value_usd": 999,
	})
	e.anon().expect(401, "GET", "/api/buyer/credit-packs", nil)

	// 之后编辑活动和兑换率均不能改变已购权益。
	seller.must("PUT", fmt.Sprintf("/api/seller/credit-offers/%d", offer.ID), map[string]any{
		"title": "改价后的活动", "price_cny": 90, "face_value_usd": 50,
		"allowed_platforms": []string{"anthropic"}, "allowed_models": []string{"claude-sonnet-4-5"}, "status": "paused",
	}, nil)
	admin.must("PUT", "/api/admin/settings", map[string]any{"credit_usd_to_balance_rate": 7}, nil)
	for range 3 {
		e.drain()
		if err := e.app.Keys.ProvisionCreditPacks(e.ctx); err != nil {
			t.Fatal(err)
		}
	}
	var owned []billing.CreditPack
	buyer.must("GET", "/api/buyer/credit-packs", nil, &owned)
	if len(owned) != 1 {
		t.Fatalf("idempotent purchase created %d packs", len(owned))
	}
	p := owned[0]
	if p.Status != "active" || p.Title != "八十元买一百美元" || p.RemainingUSD != 100 || p.RemainingBalanceUnits != 200 || p.PrepaidRemainingCNY != 80 || p.USDToBalanceRate != 2 ||
		len(p.AllowedPlatforms) != 1 || p.AllowedPlatforms[0] != "openai" || len(p.AllowedModels) != 1 || p.AllowedModels[0] != "gpt-6-astra" {
		t.Fatalf("purchased rights changed with offer/config edit: %+v", p)
	}
	var wallet billing.Wallet
	buyer.must("GET", "/api/buyer/wallet", nil, &wallet)
	if wallet.Balance == nil || math.Abs(*wallet.Balance-20) > 1e-8 || math.Abs(wallet.LedgerCash-20) > 1e-8 {
		t.Fatalf("store credits must not become platform cash: %+v", wallet)
	}
	var postings int
	mustScan(t, e.app.DB.QueryRow(e.ctx, `SELECT count(*) FROM ledger_txns WHERE ref=$1`, fmt.Sprintf("store-credit-purchase:%d", p.ID)), &postings)
	if postings != 1 {
		t.Fatalf("expected one purchase posting, got %d", postings)
	}
}
