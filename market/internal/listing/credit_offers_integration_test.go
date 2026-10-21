//go:build integration

package listing

import (
	"context"
	"errors"
	"testing"

	"github.com/yx-token/token-market/market/internal/platform/db"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
	"github.com/yx-token/token-market/market/internal/testutil"
	"github.com/yx-token/token-market/market/migrations"
)

func TestCreditOffersSellerIsolationAndScopes(t *testing.T) {
	_, pool := testutil.NewDatabase(t)
	ctx := context.Background()
	if _, err := db.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatal(err)
	}
	var seller, other int64
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('seller-credit@test.local','test') RETURNING id`).Scan(&seller); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('other-credit@test.local','test') RETURNING id`).Scan(&other); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE vendor_configs SET enabled = true WHERE platform = 'openai'`); err != nil {
		t.Fatal(err)
	}
	in := CreditOfferInput{Title: "  初始活动  ", PriceCNY: 30, FaceValueUSD: 10,
		AllowedPlatforms: []string{" openai ", "openai"}, AllowedModels: []string{" gpt-6-astra "}}
	offer, err := CreateCreditOffer(ctx, pool, seller, in)
	if err != nil {
		t.Fatal(err)
	}
	if offer.SellerID != seller || offer.Status != "active" || offer.Title != "初始活动" ||
		len(offer.AllowedPlatforms) != 1 || offer.AllowedPlatforms[0] != "openai" || offer.AllowedModels[0] != "gpt-6-astra" {
		t.Fatalf("unexpected saved offer: %+v", offer)
	}
	if _, err := CreateCreditOffer(ctx, pool, other, CreditOfferInput{Title: "其他店活动", PriceCNY: 5, FaceValueUSD: 5}); err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateCreditOffer(ctx, pool, other, offer.ID, in); !errors.Is(err, ErrCreditOfferNotFound) {
		t.Fatalf("another seller updated offer: %v", err)
	}
	if _, err := UpdateCreditOffer(ctx, pool, seller, offer.ID+10000, in); !errors.Is(err, ErrCreditOfferNotFound) {
		t.Fatalf("missing offer not hidden: %v", err)
	}
	for _, platform := range []string{"not-a-vendor", "anthropic"} {
		if platform == "anthropic" {
			if _, err := pool.Exec(ctx, `UPDATE vendor_configs SET enabled = false WHERE platform = $1`, platform); err != nil {
				t.Fatal(err)
			}
		}
		bad := in
		bad.AllowedPlatforms = []string{platform}
		_, err := CreateCreditOffer(ctx, pool, seller, bad)
		var he *httpx.Error
		if !errors.As(err, &he) || he.Code != "INVALID_CREDIT_PLATFORM" {
			t.Fatalf("invalid platform %s accepted: %v", platform, err)
		}
	}

	// Purchased value, price, and scope belong to the pack snapshot, unaffected by later edits.
	var packID int64
	if err := pool.QueryRow(ctx, `INSERT INTO store_credit_packs(offer_id,buyer_id,seller_id,title,price_cny,
		face_value_usd,remaining_usd,remaining_balance_units,prepaid_remaining_cny,usd_to_balance_rate,allowed_platforms,allowed_models,status,idempotency_key)
		SELECT id,$2,seller_id,title,price_cny,face_value_usd,face_value_usd,face_value_usd*7,price_cny,7,allowed_platforms,allowed_models,'active','snapshot-test'
		FROM store_credit_offers WHERE id=$1 RETURNING id`, offer.ID, other).Scan(&packID); err != nil {
		t.Fatal(err)
	}
	in.Title, in.PriceCNY, in.FaceValueUSD, in.Status = "修改活动", 50, 20, "paused"
	in.AllowedPlatforms, in.AllowedModels = []string{}, []string{}
	if _, err := UpdateCreditOffer(ctx, pool, seller, offer.ID, in); err != nil {
		t.Fatal(err)
	}
	in.Status = ""
	updated, err := UpdateCreditOffer(ctx, pool, seller, offer.ID, in)
	if err != nil || updated.Status != "paused" {
		t.Fatalf("editing paused offer republished it: %+v, %v", updated, err)
	}
	var price, face float64
	var title string
	var platforms, models []string
	if err := pool.QueryRow(ctx, `SELECT title,price_cny::float8,face_value_usd::float8,allowed_platforms,allowed_models
		FROM store_credit_packs WHERE id=$1`, packID).Scan(&title, &price, &face, &platforms, &models); err != nil {
		t.Fatal(err)
	}
	if title != "初始活动" || price != 30 || face != 10 || len(platforms) != 1 || platforms[0] != "openai" || len(models) != 1 || models[0] != "gpt-6-astra" {
		t.Fatalf("offer edit changed purchased snapshot: %s %v %v %v %v", title, price, face, platforms, models)
	}
	visible, err := ListCreditOffers(ctx, pool, seller, true)
	if err != nil || len(visible) != 0 || visible == nil {
		t.Fatalf("market exposed paused/other seller offer, or returned null: %+v %v", visible, err)
	}
	owned, err := ListCreditOffers(ctx, pool, seller, false)
	if err != nil || len(owned) != 1 || owned[0].ID != offer.ID {
		t.Fatalf("seller list ownership/status filter: %+v %v", owned, err)
	}
	if _, err := ListCreditOffers(ctx, pool, 0, true); err == nil {
		t.Fatal("unfiltered market listing accepted")
	}
	if _, err := GetCreditOffer(ctx, pool, offer.ID+10000); !errors.Is(err, ErrCreditOfferNotFound) {
		t.Fatalf("get missing offer: %v", err)
	}
}
