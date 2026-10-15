//go:build integration

package e2e

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/DueGin/token-market/internal/buyer"
	"github.com/DueGin/token-market/internal/edgecfg"
	"github.com/DueGin/token-market/internal/listing"
	"github.com/DueGin/token-market/internal/market"
)

// rawMarketCall 有意不使用旧场景的自动商家/检测夹具，验证真实买家与卖家流程。
func rawMarketCall(t *testing.T, c *client, status int, method, path string, body, out any) string {
	t.Helper()
	got, raw := c.call(method, path, body, out)
	if got != status {
		t.Fatalf("%s %s: got %d want %d: %s", method, path, got, status, raw)
	}
	return raw
}

func uncheckedListing(t *testing.T, e *env, seller *client, name string) (int64, int64) {
	t.Helper()
	var u listing.Upstream
	rawMarketCall(t, seller, 201, "POST", "/api/seller/upstreams", map[string]string{"platform": "anthropic", "auth_type": "oauth"}, &u)
	base := fmt.Sprintf("/api/seller/upstreams/%d", u.ID)
	rawMarketCall(t, seller, 409, "POST", base+"/listing", map[string]any{"mode": "consignment", "multiplier": .5, "categories": []string{"coding"}}, nil)
	rawMarketCall(t, seller, 200, "POST", base+"/oauth/start", nil, nil)
	rawMarketCall(t, seller, 200, "POST", base+"/oauth/complete", map[string]string{"code": name}, &u)
	if u.Status != "selecting_models" {
		t.Fatalf("authorization must still require model selection: %+v", u)
	}
	rawMarketCall(t, seller, 200, "PUT", base+"/models", map[string]any{"models": []string{"gpt-6-astra", "gpt-6-sol"}}, &u)
	if u.Status != "ready" || u.Health != "unknown" || u.DetectionStatus != "untested" || u.QualityFactor != nil || u.LastCheckedAt != nil {
		t.Fatalf("selected authorized account must be ready with no fabricated quality evidence: %+v", u)
	}
	var runs int
	mustScan(t, e.app.DB.QueryRow(e.ctx, "SELECT count(*) FROM probe_runs WHERE upstream_id = $1", u.ID), &runs)
	if runs != 0 {
		t.Fatalf("selecting models must not run detection: %d", runs)
	}
	rawMarketCall(t, seller, 409, "POST", base+"/listing", map[string]any{"mode": "buyback"}, nil)
	var created struct {
		Listing listing.Listing `json:"listing"`
	}
	rawMarketCall(t, seller, 201, "POST", base+"/listing", map[string]any{"mode": "consignment", "multiplier": .5, "categories": []string{"coding"}}, &created)
	return u.ID, created.Listing.ID
}

func TestMerchantListOwnershipOrderAndImmediateRemoval(t *testing.T) {
	e := setup(t)
	seller := prepareModelWhitelistSeller(e, "merchant-owner", 2)
	e.fake.SetAccountModels(0, []string{"gpt-6-astra", "gpt-6-sol"})
	_, first := uncheckedListing(t, e, seller, "merchant-one")
	_, second := uncheckedListing(t, e, seller, "merchant-two")
	e.schedule()
	var sellerID int64
	mustScan(t, e.app.DB.QueryRow(e.ctx, "SELECT seller_id FROM listings WHERE id=$1", first), &sellerID)
	buyer1, buyer2 := e.newUser("merchant-buyer1@market.test"), e.newUser("merchant-buyer2@market.test")
	rawMarketCall(t, e.anon(), 401, "GET", "/api/buyer/merchants", nil, nil)
	input := buyer.KeyInput{Name: "ordered", Route: buyer.RouteInput{Type: "chain", Hops: []buyer.HopInput{{ListingID: second}, {ListingID: first}}}}
	if raw := rawMarketCall(t, buyer1, 400, "POST", "/api/buyer/keys", input, nil); !strings.Contains(raw, "MERCHANT_NOT_SELECTED") {
		t.Fatalf("missing merchant must be rejected explicitly: %s", raw)
	}
	path := fmt.Sprintf("/api/buyer/merchants/%d", sellerID)
	rawMarketCall(t, buyer1, 200, "PUT", path, nil, nil)
	rawMarketCall(t, buyer1, 200, "PUT", path, nil, nil)
	var merchants []struct {
		buyer.Merchant
		Listings []market.Item `json:"listings"`
	}
	rawMarketCall(t, buyer1, 200, "GET", "/api/buyer/merchants", nil, &merchants)
	if len(merchants) != 1 || len(merchants[0].Listings) != 2 || merchants[0].SellerID != sellerID {
		t.Fatalf("merchant identity must deduplicate across listings: %+v", merchants)
	}
	for _, it := range merchants[0].Listings {
		if it.DetectionStatus != "untested" || len(it.ModelChecks) != 2 || it.ModelChecks[0].DegradationStatus != "untested" || it.ModelChecks[1].DegradationStatus != "" {
			t.Fatalf("buyer model and astra degradation display must preserve untested: %+v", it)
		}
	}
	rawMarketCall(t, buyer2, 400, "POST", "/api/buyer/keys", input, nil)
	var made struct {
		Key    string         `json:"key"`
		Detail buyer.BuyerKey `json:"detail"`
	}
	rawMarketCall(t, buyer1, 201, "POST", "/api/buyer/keys", input, &made)
	read := func() *edgecfg.KeyConfig {
		t.Helper()
		cfg, err := e.app.Keys.Edge.Get(e.ctx, edgecfg.HashKey(made.Key))
		if err != nil {
			t.Fatal(err)
		}
		return cfg
	}
	if cfg := read(); len(cfg.Hops) != 2 || cfg.Hops[0].ListingID != second || cfg.Hops[1].ListingID != first {
		t.Fatalf("published hops must preserve explicit priority: %+v", cfg)
	}
	reversed := buyer.RouteInput{Type: "chain", Hops: []buyer.HopInput{{ListingID: first}, {ListingID: second}}}
	rawMarketCall(t, buyer1, 200, "PUT", fmt.Sprintf("/api/buyer/keys/%d", made.Detail.ID), buyer.UpdateKeyInput{Route: &reversed}, nil)
	if cfg := read(); cfg.Hops[0].ListingID != first {
		t.Fatalf("editing priority must change published order: %+v", cfg)
	}
	rawMarketCall(t, buyer2, 200, "DELETE", path, nil, nil)
	if cfg := read(); len(cfg.Hops) != 2 {
		t.Fatal("another buyer's removal must not affect this key")
	}
	rawMarketCall(t, buyer1, 200, "DELETE", path, nil, nil)
	if cfg := read(); len(cfg.Hops) != 0 || cfg.Status != "disabled" {
		t.Fatalf("removing merchant must immediately stop all its listings: %+v", cfg)
	}
	rawMarketCall(t, buyer1, 200, "DELETE", path, nil, nil)
	rawMarketCall(t, buyer1, 200, "PUT", path, nil, nil)
	if cfg := read(); len(cfg.Hops) != 2 || cfg.Hops[0].ListingID != first {
		t.Fatalf("re-adding the seller must preserve saved order: %+v", cfg)
	}
	if _, err := e.app.DB.Exec(e.ctx, "UPDATE listings SET status='paused' WHERE id=$1", first); err != nil {
		t.Fatal(err)
	}
	if err := e.app.Keys.Publish(e.ctx, made.Detail.ID); err != nil {
		t.Fatal(err)
	}
	if cfg := read(); len(cfg.Hops) != 1 || cfg.Hops[0].ListingID != second {
		t.Fatalf("paused listing must be filtered without disturbing other hops: %+v", cfg)
	}
	rawMarketCall(t, buyer1, 400, "PUT", fmt.Sprintf("/api/buyer/keys/%d", made.Detail.ID), buyer.UpdateKeyInput{Route: &reversed}, nil)
}

func TestUncheckedListingCannotBypassDisabledOrSuspendedState(t *testing.T) {
	e := setup(t)
	seller := prepareModelWhitelistSeller(e, "unchecked-states", 1)
	e.fake.SetAccountModels(0, []string{"gpt-6-astra", "gpt-6-sol"})
	upstreamID, listingID := uncheckedListing(t, e, seller, "unchecked-state-owner")
	base := fmt.Sprintf("/api/seller/upstreams/%d", upstreamID)
	for _, status := range []string{"suspended", "removing", "removed", "draft"} {
		if _, err := e.app.DB.Exec(e.ctx, "UPDATE upstreams SET status=$2 WHERE id=$1", upstreamID, status); err != nil {
			t.Fatal(err)
		}
		rawMarketCall(t, seller, http.StatusConflict, "POST", base+"/retry-verification", nil, nil)
		rawMarketCall(t, seller, http.StatusConflict, "POST", base+"/listing", map[string]any{"mode": "consignment", "multiplier": .5, "categories": []string{"coding"}}, nil)
		rawMarketCall(t, e.anon(), http.StatusNotFound, "GET", fmt.Sprintf("/api/market/listings/%d", listingID), nil, nil)
	}
}

func TestReferenceModelSelectionStillRunsVerification(t *testing.T) {
	e := setup(t)
	seller := prepareModelWhitelistSeller(e, "reference-selection", 1)
	e.fake.SetAccountModels(0, []string{whitelistSonnet})
	var u listing.Upstream
	rawMarketCall(t, seller, 201, "POST", "/api/seller/upstreams", map[string]string{"platform": "anthropic", "auth_type": "oauth"}, &u)
	base := fmt.Sprintf("/api/seller/upstreams/%d", u.ID)
	rawMarketCall(t, seller, 200, "POST", base+"/oauth/start", nil, nil)
	rawMarketCall(t, seller, 200, "POST", base+"/oauth/complete", map[string]string{"code": "reference-selected-model"}, &u)
	// 覆盖存量/管理员配置中基线账号已经进入模型选择状态的分支。
	if _, err := e.app.DB.Exec(e.ctx, `UPDATE upstreams SET is_reference=true WHERE id=$1`, u.ID); err != nil {
		t.Fatal(err)
	}
	rawMarketCall(t, seller, 200, "PUT", base+"/models", map[string]any{"models": []string{whitelistSonnet}}, &u)
	if u.Status != "verifying" || u.DetectionStatus != "untested" {
		t.Fatalf("reference model selection must queue verification without claiming a result: %+v", u)
	}
	var runs int
	mustScan(t, e.app.DB.QueryRow(e.ctx, `SELECT count(*) FROM probe_runs WHERE upstream_id=$1 AND trigger='onboarding'`, u.ID), &runs)
	if runs != 1 {
		t.Fatalf("reference account must still queue exactly one verification: %d", runs)
	}
	rawMarketCall(t, seller, 400, "POST", base+"/listing", map[string]any{"mode": "consignment", "multiplier": .5, "categories": []string{"coding"}}, nil)
}
