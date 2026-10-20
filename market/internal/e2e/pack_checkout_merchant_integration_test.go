//go:build integration

package e2e

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/yx-token/token-market/market/internal/billing"
	"github.com/yx-token/token-market/market/internal/buyer"
	"github.com/yx-token/token-market/market/internal/listing"
)

type packCheckoutRechargeView struct {
	rechargeView
	PackCheckoutStatus *string `json:"pack_checkout_status"`
}

// 同时跑两个履约 worker，并在终态后重跑，覆盖竞争认领和重试。
func runPackCheckoutWorkers(t *testing.T, e *env) {
	t.Helper()
	start := make(chan struct{})
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			results <- e.app.Keys.FulfillPackCheckouts(e.ctx)
		}()
	}
	close(start)
	for i := 0; i < 2; i++ {
		if err := <-results; err != nil {
			t.Errorf("fulfill pack checkouts: %v", err)
		}
	}
	if t.Failed() {
		t.FailNow()
	}
}

func TestPackCheckoutMerchantInvalidation(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		remove    bool
		faultAt   string
		errorCode string
	}{
		{name: "merchant_removed", remove: true, errorCode: "MERCHANT_NOT_SELECTED"},
		{name: "listing_paused", errorCode: "LISTING_UNAVAILABLE"},
		{name: "pack_write_failed", faultAt: "insert", errorCode: "PACK_CHECKOUT_WRITE_FAILED"},
		{name: "pack_commit_failed", faultAt: "commit", errorCode: "PACK_CHECKOUT_WRITE_FAILED"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			_, configure := withPayments(t)
			e := setup(t)
			configure(e, e.login("admin@market.test", "admin-password-1"))
			seller := prepareModelWhitelistSeller(e, "pack-checkout-seller", 1)
			e.fake.SetAccountModels(0, []string{whitelistSonnet})
			var upstream listing.Upstream
			rawMarketCall(t, seller, http.StatusCreated, "POST", "/api/seller/upstreams",
				map[string]string{"platform": "anthropic", "auth_type": "oauth"}, &upstream)
			base := fmt.Sprintf("/api/seller/upstreams/%d", upstream.ID)
			rawMarketCall(t, seller, http.StatusOK, "POST", base+"/oauth/start", nil, nil)
			rawMarketCall(t, seller, http.StatusOK, "POST", base+"/oauth/complete",
				map[string]string{"code": "pack-checkout-" + scenario.name}, &upstream)
			rawMarketCall(t, seller, http.StatusOK, "PUT", base+"/models",
				map[string]any{"models": []string{whitelistSonnet}}, &upstream)
			var made struct {
				Listing listing.Listing `json:"listing"`
			}
			rawMarketCall(t, seller, http.StatusCreated, "POST", base+"/listing", map[string]any{
				"mode": "pack", "multiplier": .5, "face_value": 10, "stock": 4, "categories": []string{"coding"},
			}, &made)
			item := made.Listing
			e.schedule()
			merchantPath := fmt.Sprintf("/api/buyer/merchants/%d", item.SellerID)
			stockSold := func(want int) {
				t.Helper()
				var got int
				mustScan(t, e.app.DB.QueryRow(e.ctx, "SELECT stock_sold FROM listings WHERE id=$1", item.ID), &got)
				if got != want {
					t.Fatalf("stock_sold=%d, want %d", got, want)
				}
			}
			purchase := func(c *client, name string) *buyer.PackPayOrder {
				t.Helper()
				// 使用原始 HTTP，显式选择商家，避免旧夹具自动修复权限。
				rawMarketCall(t, c, http.StatusOK, "PUT", merchantPath, nil, nil)
				var result buyer.PurchasePackResult
				rawMarketCall(t, c, http.StatusCreated, "POST", "/api/buyer/packs", buyer.PurchasePackInput{
					ListingID: item.ID, Channel: "alipay_page", Name: name,
				}, &result)
				if result.Recharge == nil || result.Recharge.Payment == nil || result.Recharge.Payment.URL == "" {
					t.Fatalf("online purchase must reserve stock and return payment: %+v", result)
				}
				return result.Recharge
			}
			readOrder := func(c *client, id int64, status, checkoutStatus string) {
				t.Helper()
				var order packCheckoutRechargeView
				rawMarketCall(t, c, http.StatusOK, "GET", "/api/buyer/recharge/"+itoa(id), nil, &order)
				if order.ID != id || order.Status != status || order.PackCheckoutStatus == nil || *order.PackCheckoutStatus != checkoutStatus {
					t.Fatalf("recharge/checkout state: %+v, want %s/%s", order, status, checkoutStatus)
				}
			}
			pay := func(order *buyer.PackPayOrder) {
				t.Helper()
				resp, body := browse(t, order.Payment.URL)
				if resp.StatusCode != http.StatusFound {
					t.Fatalf("fake payment failed: %d %s", resp.StatusCode, body)
				}
				e.settle()
				if got := e.orderStatus(order.ID); got != "credited" {
					t.Fatalf("payment must actually credit before fulfillment: %s", got)
				}
			}

			// 先正常履约一份旧包：保留正的已售库存，防止 GREATEST(..., 0) 掩盖重复释放。
			owner := e.newUser("pack-checkout-owner@market.test")
			good := purchase(owner, "normal online pack")
			stockSold(1)
			readOrder(owner, good.ID, "pending", "pending")
			pay(good)
			readOrder(owner, good.ID, "credited", "pending")
			runPackCheckoutWorkers(t, e)
			readOrder(owner, good.ID, "credited", "fulfilled")
			stockSold(1)
			var packs []listing.QuotaPack
			var keys []buyer.BuyerKey
			rawMarketCall(t, owner, http.StatusOK, "GET", "/api/buyer/packs", nil, &packs)
			rawMarketCall(t, owner, http.StatusOK, "GET", "/api/buyer/keys", nil, &keys)
			if len(packs) != 1 || len(keys) != 1 || packs[0].BuyerKeyID == nil || *packs[0].BuyerKeyID != keys[0].ID ||
				packs[0].RechargeID == nil || *packs[0].RechargeID != good.ID || packs[0].ListingID != item.ID ||
				packs[0].Status != "active" || !approx(packs[0].Remaining, 10) || keys[0].Status != "active" {
				t.Fatalf("normal fulfillment must issue exactly one active pack/key: packs=%+v keys=%+v", packs, keys)
			}
			var normalWallet billing.Wallet
			rawMarketCall(t, owner, http.StatusOK, "GET", "/api/buyer/wallet", nil, &normalWallet)
			if normalWallet.Balance == nil || !approx(*normalWallet.Balance, 10) || !approx(normalWallet.LedgerCash, 0) {
				t.Fatalf("normal pack must retain calling balance and post prepayment once: %+v", normalWallet)
			}

			payer := e.newUser("pack-checkout-payer@market.test")
			bad := purchase(payer, "invalidated while awaiting payment")
			stockSold(2)
			readOrder(payer, bad.ID, "pending", "pending")
			if scenario.remove {
				rawMarketCall(t, payer, http.StatusOK, "DELETE", merchantPath, nil, nil)
			} else if scenario.faultAt == "" {
				rawMarketCall(t, seller, http.StatusOK, "POST", fmt.Sprintf("/api/seller/listings/%d/pause", item.ID), nil, nil)
			} else {
				// 在真实 PG 中注入写入/延迟提交错误，确保 Create 已提交的 Key 不会残留可用。
				if _, err := e.app.DB.Exec(e.ctx, `CREATE FUNCTION reject_pack_checkout_write() RETURNS trigger
					LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'PACK_CHECKOUT_WRITE_FAILED'; END $$`); err != nil {
					t.Fatal(err)
				}
				trigger := `CREATE TRIGGER reject_pack_checkout_write BEFORE INSERT ON quota_packs
					FOR EACH ROW EXECUTE FUNCTION reject_pack_checkout_write()`
				if scenario.faultAt == "commit" {
					trigger = `CREATE CONSTRAINT TRIGGER reject_pack_checkout_write AFTER INSERT ON quota_packs
						DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION reject_pack_checkout_write()`
				}
				if _, err := e.app.DB.Exec(e.ctx, trigger); err != nil {
					t.Fatal(err)
				}
			}
			pay(bad)
			readOrder(payer, bad.ID, "credited", "pending")
			runPackCheckoutWorkers(t, e)
			readOrder(payer, bad.ID, "credited", "failed")
			stockSold(1)
			var packID *int64
			var reason string
			mustScan(t, e.app.DB.QueryRow(e.ctx, "SELECT pack_id, error_reason FROM pack_checkouts WHERE recharge_id=$1", bad.ID), &packID, &reason)
			if packID != nil || !strings.Contains(reason, scenario.errorCode) {
				t.Fatalf("failed checkout must retain its permission error and have no pack: id=%v reason=%q", packID, reason)
			}
			rawMarketCall(t, owner, http.StatusNotFound, "GET", "/api/buyer/recharge/"+itoa(bad.ID), nil, nil)
			rawMarketCall(t, payer, http.StatusNotFound, "GET", "/api/buyer/recharge/"+itoa(good.ID), nil, nil)

			for attempt := 0; attempt < 2; attempt++ {
				runPackCheckoutWorkers(t, e)
			}
			stockSold(1)
			readOrder(payer, bad.ID, "credited", "failed")
			var payerID int64
			mustScan(t, e.app.DB.QueryRow(e.ctx, "SELECT buyer_id FROM pack_checkouts WHERE recharge_id=$1", bad.ID), &payerID)
			for _, query := range []string{
				"SELECT count(*) FROM quota_packs WHERE buyer_id=$1",
				"SELECT count(*) FROM buyer_keys WHERE user_id=$1 AND status<>'deleted'",
				"SELECT count(*) FROM refunds WHERE user_id=$1",
			} {
				var count int
				mustScan(t, e.app.DB.QueryRow(e.ctx, query, payerID), &count)
				if count != 0 {
					t.Fatalf("failed checkout must not issue packs/keys or refund credited money: %s: %d", query, count)
				}
			}
			var keyRows int
			mustScan(t, e.app.DB.QueryRow(e.ctx, "SELECT count(*) FROM buyer_keys WHERE user_id=$1", payerID), &keyRows)
			if scenario.faultAt == "" && keyRows != 0 {
				t.Fatalf("permission failures must not create any key: %d", keyRows)
			}
			if scenario.faultAt != "" {
				if keyRows < 1 || keyRows > 2 {
					t.Fatalf("write failure must clean every key created by the two competing workers: %d", keyRows)
				}
				rows, err := e.app.DB.Query(e.ctx, "SELECT encode(key_hash, 'hex') FROM buyer_keys WHERE user_id=$1 AND status='deleted'", payerID)
				if err != nil {
					t.Fatal(err)
				}
				defer rows.Close()
				for rows.Next() {
					var keyHash string
					mustScan(t, rows, &keyHash)
					if cfg, err := e.app.Keys.Edge.Get(e.ctx, keyHash); err == nil || cfg != nil {
						t.Fatalf("orphan key must be removed from Edge: %+v %v", cfg, err)
					}
				}
				if err := rows.Err(); err != nil {
					t.Fatal(err)
				}
			}
			var wallet billing.Wallet
			rawMarketCall(t, payer, http.StatusOK, "GET", "/api/buyer/wallet", nil, &wallet)
			if wallet.Balance == nil || wallet.Refundable == nil || !approx(*wallet.Balance, 10) ||
				!approx(wallet.LedgerCash, 10) || !approx(*wallet.Refundable, 10) {
				t.Fatalf("paid CNY must remain exactly once in ordinary balance after retries: %+v", wallet)
			}
			var selected bool
			mustScan(t, e.app.DB.QueryRow(e.ctx, "SELECT EXISTS(SELECT 1 FROM buyer_merchants WHERE user_id=$1 AND seller_id=$2)", payerID, item.SellerID), &selected)
			if selected == scenario.remove {
				t.Fatalf("fulfillment must respect the buyer's merchant selection: selected=%v removed=%v", selected, scenario.remove)
			}
			// 普通充值不混入额度包状态字段。
			var ordinary struct {
				Order rechargeView `json:"order"`
			}
			rawMarketCall(t, payer, http.StatusCreated, "POST", "/api/buyer/recharge", map[string]any{"amount": 10, "channel": "manual"}, &ordinary)
			var ordinaryView packCheckoutRechargeView
			rawMarketCall(t, payer, http.StatusOK, "GET", "/api/buyer/recharge/"+itoa(ordinary.Order.ID), nil, &ordinaryView)
			if ordinaryView.PackCheckoutStatus != nil {
				t.Fatalf("ordinary recharge must omit pack_checkout_status: %+v", ordinaryView)
			}
			// 旧包正常购入和退款均须真实入账，数据库约束不能拒绝既有交易类型。
			var refunded listing.QuotaPack
			refundPath := "/api/buyer/packs/" + itoa(packs[0].ID) + "/refund"
			rawMarketCall(t, owner, http.StatusOK, "POST", refundPath, map[string]string{"dest": "balance"}, &refunded)
			if refunded.Status != "refunded" || refunded.Remaining != 0 {
				t.Fatalf("normal unused pack must refund: %+v", refunded)
			}
			rawMarketCall(t, owner, http.StatusConflict, "POST", refundPath, map[string]string{"dest": "balance"}, nil)
			stockSold(0)
			var refundedWallet billing.Wallet
			rawMarketCall(t, owner, http.StatusOK, "GET", "/api/buyer/wallet", nil, &refundedWallet)
			if refundedWallet.Balance == nil || !approx(*refundedWallet.Balance, 10) || !approx(refundedWallet.LedgerCash, 10) {
				t.Fatalf("normal pack refund must restore prepayment exactly once: %+v", refundedWallet)
			}
			for _, typ := range []string{"pack_purchase", "pack_refund"} {
				var count int
				mustScan(t, e.app.DB.QueryRow(e.ctx, "SELECT count(*) FROM ledger_txns WHERE type=$1", typ), &count)
				if count != 1 {
					t.Fatalf("normal pack must post %s once: %d", typ, count)
				}
			}
		})
	}
}
