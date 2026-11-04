//go:build integration

package edge

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/yx-token/token-market/market/internal/platform/db"
	"github.com/yx-token/token-market/market/internal/testutil"
	"github.com/yx-token/token-market/market/migrations"
)

type usageDBFixture struct {
	pool                          *pgxpool.Pool
	buyerID, otherBuyerID         int64
	sellerID, otherSellerID       int64
	keyID, otherKeyID             int64
	listingIDs                    []int64
	packID, offerID, fundingID    int64
	buyerShadowID, creditShadowID int64
}

func (f *usageDBFixture) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	// Subtest contexts are canceled before Cleanup restores the shared fixture.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 5*time.Second)
	defer cancel()
	_, err := f.pool.Exec(ctx, query, args...)
	require.NoError(t, err, query)
}

func (f *usageDBFixture) id(t *testing.T, query string, args ...any) int64 {
	t.Helper()
	var id int64
	require.NoError(t, f.pool.QueryRow(t.Context(), query, args...).Scan(&id), query)
	return id
}

func (f *usageDBFixture) read(t *testing.T) UsageSnapshot {
	t.Helper()
	got, err := NewDBUsageReader(f.pool).ReadUsage(t.Context(), f.keyID, f.buyerID)
	require.NoError(t, err)
	return got
}

func newUsageDBFixture(t *testing.T) *usageDBFixture {
	t.Helper()
	_, pool := testutil.NewDatabase(t)
	_, err := db.Migrate(t.Context(), pool, migrations.FS)
	require.NoError(t, err)
	f := &usageDBFixture{pool: pool}
	newUser := func(email string) int64 {
		return f.id(t, `INSERT INTO users(email,password_hash) VALUES($1,'test') RETURNING id`, email)
	}
	f.buyerID, f.otherBuyerID = newUser("usage-buyer@test"), newUser("usage-other-buyer@test")
	f.sellerID, f.otherSellerID = newUser("usage-seller@test"), newUser("usage-other-seller@test")
	newKey := func(userID int64, name string) int64 {
		hash := sha256.Sum256([]byte(name))
		return f.id(t, `INSERT INTO buyer_keys(user_id,name,key_hash,key_prefix,route_type,status)
		 VALUES($1,$2,$3,'tm-usage','chain','active') RETURNING id`, userID, name, hash[:])
	}
	f.keyID, f.otherKeyID = newKey(f.buyerID, "tm-usage"), newKey(f.otherBuyerID, "tm-other")
	f.buyerShadowID = f.id(t, `INSERT INTO shadow_identities(kind,owner_id,status,sub2api_user_id,email,password_enc)
	 VALUES('buyer',$1,'active',101,'usage-buyer@shadow','test') RETURNING id`, f.buyerID)
	f.exec(t, `UPDATE vendor_configs SET enabled=true WHERE platform='anthropic'`)
	for i := 0; i < 2; i++ {
		upstreamID := f.id(t, `INSERT INTO upstreams(seller_id,platform,auth_type,status,sub2api_account_id,model_whitelist)
		 VALUES($1,'anthropic','apikey','listed',$2,'{match,other}') RETURNING id`, f.sellerID, 1001+i)
		listingID := f.id(t, `INSERT INTO listings(upstream_id,seller_id,mode,status,multiplier,listing_group_id,listing_group_rate)
		 VALUES($1,$2,'consignment','active',1,$3,1) RETURNING id`, upstreamID, f.sellerID, 2001+i)
		f.listingIDs = append(f.listingIDs, listingID)
		f.exec(t, `INSERT INTO buyer_key_hops(key_id,position,listing_id) VALUES($1,$2,$3)`, f.keyID, i+1, listingID)
		hash := sha256.Sum256([]byte(fmt.Sprintf("internal-%d", i)))
		f.exec(t, `INSERT INTO internal_keys(buyer_key_id,buyer_user_id,sub2api_group_id,sub2api_key_id,key_enc,key_hash,status)
		 VALUES($1,$2,$3,$4,'test',$5,'active')`, f.keyID, f.buyerID, 2001+i, 3001+i, hash[:])
	}
	f.exec(t, `INSERT INTO buyer_merchants(user_id,seller_id) VALUES($1,$2)`, f.buyerID, f.sellerID)
	// Both entries commit in one statement so the real deferred balance trigger runs.
	f.exec(t, `WITH txn AS (
	 INSERT INTO ledger_txns(type,ref) VALUES('recharge','usage-test-wallet') RETURNING id
	), accounts AS (
	 INSERT INTO ledger_accounts(owner_type,owner_id,kind)
	 VALUES('buyer',$1,'buyer_cash'),('platform',0,'platform_cash') RETURNING id,kind
	)
	INSERT INTO ledger_entries(txn_id,account_id,amount)
	SELECT txn.id,accounts.id,CASE WHEN accounts.kind='buyer_cash' THEN -25.5 ELSE 25.5 END FROM txn CROSS JOIN accounts`, f.buyerID)
	return f
}

func (f *usageDBFixture) addStoreCredit(t *testing.T) {
	t.Helper()
	f.offerID = f.id(t, `INSERT INTO store_credit_offers(seller_id,title,price_cny,face_value_usd,allowed_platforms,allowed_models)
	 VALUES($1,'usage-credit',80,100,'{anthropic}','{match}') RETURNING id`, f.sellerID)
	f.fundingID = f.id(t, `INSERT INTO sub2api_ops(op_type,target,payload,idempotency_key,status)
	 VALUES('user_balance_add','usage-credit','{}','usage-credit-funding','done') RETURNING id`)
	f.packID = f.id(t, `INSERT INTO store_credit_packs(offer_id,buyer_id,seller_id,title,price_cny,face_value_usd,
	 remaining_usd,remaining_balance_units,prepaid_remaining_cny,usd_to_balance_rate,allowed_platforms,allowed_models,
	 idempotency_key,status,funding_op_id)
	 VALUES($1,$2,$3,'usage-credit',80,100,40,80,32,2,'{anthropic}','{match}','usage-credit-purchase','active',$4) RETURNING id`,
		f.offerID, f.buyerID, f.sellerID, f.fundingID)
	f.creditShadowID = f.id(t, `INSERT INTO shadow_identities(kind,owner_id,status,sub2api_user_id,email,password_enc)
	 VALUES('credit_pack',$1,'active',102,'usage-credit@shadow','test') RETURNING id`, f.packID)
	for i, listingID := range f.listingIDs {
		hash := sha256.Sum256([]byte(fmt.Sprintf("credit-%d", i)))
		f.exec(t, `INSERT INTO store_credit_keys(pack_id,buyer_key_id,listing_id,sub2api_group_id,sub2api_key_id,key_enc,key_hash,status)
		 VALUES($1,$2,$3,$4,$5,'test',$6,'active')`, f.packID, f.keyID, listingID, 2001+i, 4001+i, hash[:])
	}
}

func TestDBUsageReaderBalancesAndScope(t *testing.T) {
	f := newUsageDBFixture(t)
	t.Run("wallet uses buyer cash liability sign", func(t *testing.T) {
		got := f.read(t)
		require.Equal(t, 25.5, got.Cash)
		require.False(t, got.IsQuotaPack)
		require.Zero(t, got.QuotaPack)
		require.Zero(t, got.StoreCredit)
		require.Nil(t, got.QuotaLimit)
	})
	f.addStoreCredit(t)
	t.Run("same activity on two routes is counted once", func(t *testing.T) {
		got := f.read(t)
		require.Equal(t, 25.5, got.Cash)
		require.Equal(t, 80.0, got.StoreCredit)
		require.Equal(t, 40.0, got.StoreCreditUSD)
		require.Equal(t, 32.0, got.StoreCreditPrepaidCNY)
	})
	for _, status := range []string{"active", "depleted", "refunding", "refunded"} {
		t.Run("legacy quota pack "+status+" retains dedicated key identity", func(t *testing.T) {
			f.exec(t, `INSERT INTO quota_packs(listing_id,buyer_id,buyer_key_id,face_value,remaining,status,paid_amount)
			 VALUES($1,$2,$3,10,6.25,$4,10)`, f.listingIDs[0], f.buyerID, f.keyID, status)
			t.Cleanup(func() { f.exec(t, `DELETE FROM quota_packs WHERE buyer_key_id=$1`, f.keyID) })
			got := f.read(t)
			require.True(t, got.IsQuotaPack, "inactive dedicated pack must not become a wallet key")
			if status == "active" {
				require.Equal(t, 6.25, got.QuotaPack)
			} else {
				require.Zero(t, got.QuotaPack)
			}
			require.Zero(t, got.StoreCredit, "dedicated legacy key must not use store credits")
		})
	}
	for _, status := range []string{"provisioning", "depleted", "refunding", "refunded"} {
		t.Run("activity pack "+status+" is unavailable", func(t *testing.T) {
			f.exec(t, `UPDATE store_credit_packs SET status=$1 WHERE id=$2`, status, f.packID)
			t.Cleanup(func() { f.exec(t, `UPDATE store_credit_packs SET status='active' WHERE id=$1`, f.packID) })
			require.Zero(t, f.read(t).StoreCredit)
		})
	}
	restrictions := []struct {
		name, apply, restore string
		args                 []any
	}{
		{"credit credential frozen", `UPDATE store_credit_keys SET status='frozen' WHERE pack_id=$1`, `UPDATE store_credit_keys SET status='active' WHERE pack_id=$1`, []any{f.packID}},
		{"credit credential not provisioned", `UPDATE store_credit_keys SET sub2api_key_id=NULL WHERE pack_id=$1`, `UPDATE store_credit_keys SET sub2api_key_id=4000+listing_id WHERE pack_id=$1`, []any{f.packID}},
		{"credit group mismatch", `UPDATE store_credit_keys SET sub2api_group_id=9999 WHERE pack_id=$1`, `UPDATE store_credit_keys ck SET sub2api_group_id=l.listing_group_id FROM listings l WHERE ck.listing_id=l.id AND ck.pack_id=$1`, []any{f.packID}},
		{"ordinary credential frozen", `UPDATE internal_keys SET status='frozen' WHERE buyer_key_id=$1`, `UPDATE internal_keys SET status='active' WHERE buyer_key_id=$1`, []any{f.keyID}},
		{"funding incomplete", `UPDATE sub2api_ops SET status='pending' WHERE id=$1`, `UPDATE sub2api_ops SET status='done' WHERE id=$1`, []any{f.fundingID}},
		{"funding failed permanently", `UPDATE sub2api_ops SET status='dead' WHERE id=$1`, `UPDATE sub2api_ops SET status='done' WHERE id=$1`, []any{f.fundingID}},
		{"credit shadow locked", `UPDATE shadow_identities SET keys_locked=true WHERE id=$1`, `UPDATE shadow_identities SET keys_locked=false WHERE id=$1`, []any{f.creditShadowID}},
		{"credit shadow inactive", `UPDATE shadow_identities SET status='failed' WHERE id=$1`, `UPDATE shadow_identities SET status='active' WHERE id=$1`, []any{f.creditShadowID}},
		{"listing paused", `UPDATE listings SET status='paused' WHERE seller_id=$1`, `UPDATE listings SET status='active' WHERE seller_id=$1`, []any{f.sellerID}},
		{"upstream unavailable", `UPDATE upstreams SET health='unavailable' WHERE seller_id=$1`, `UPDATE upstreams SET health='unknown' WHERE seller_id=$1`, []any{f.sellerID}},
		{"upstream unlisted", `UPDATE upstreams SET status='suspended' WHERE seller_id=$1`, `UPDATE upstreams SET status='listed' WHERE seller_id=$1`, []any{f.sellerID}},
		{"reference upstream", `UPDATE upstreams SET is_reference=true WHERE seller_id=$1`, `UPDATE upstreams SET is_reference=false WHERE seller_id=$1`, []any{f.sellerID}},
		{"vendor disabled", `UPDATE vendor_configs SET enabled=false WHERE platform='anthropic'`, `UPDATE vendor_configs SET enabled=true WHERE platform='anthropic'`, nil},
		{"platform mismatch", `UPDATE store_credit_packs SET allowed_platforms='{openai}' WHERE id=$1`, `UPDATE store_credit_packs SET allowed_platforms='{anthropic}' WHERE id=$1`, []any{f.packID}},
		{"model intersection empty", `UPDATE upstreams SET model_whitelist='{different}' WHERE seller_id=$1`, `UPDATE upstreams SET model_whitelist='{match,other}' WHERE seller_id=$1`, []any{f.sellerID}},
		{"merchant relationship removed", `DELETE FROM buyer_merchants WHERE user_id=$1 AND seller_id=$2`, `INSERT INTO buyer_merchants(user_id,seller_id) VALUES($1,$2)`, []any{f.buyerID, f.sellerID}},
		{"not buyer facing", `INSERT INTO upstream_runtime_params(upstream_id,buyer_facing,group_ids,priority,concurrency,rate_multiplier) SELECT id,false,'{}',1,1,1 FROM upstreams WHERE seller_id=$1`, `DELETE FROM upstream_runtime_params WHERE upstream_id IN (SELECT id FROM upstreams WHERE seller_id=$1)`, []any{f.sellerID}},
	}
	for _, restriction := range restrictions {
		t.Run(restriction.name, func(t *testing.T) {
			f.exec(t, restriction.apply, restriction.args...)
			t.Cleanup(func() { f.exec(t, restriction.restore, restriction.args...) })
			got := f.read(t)
			require.Equal(t, 25.5, got.Cash, "unusable activity must not erase ordinary wallet funds")
			require.Zero(t, got.StoreCredit)
			require.Zero(t, got.StoreCreditUSD)
			require.Zero(t, got.StoreCreditPrepaidCNY)
		})
	}
	t.Run("pack buyer mismatch", func(t *testing.T) {
		f.exec(t, `UPDATE store_credit_packs SET buyer_id=$2 WHERE id=$1`, f.packID, f.otherBuyerID)
		t.Cleanup(func() { f.exec(t, `UPDATE store_credit_packs SET buyer_id=$2 WHERE id=$1`, f.packID, f.buyerID) })
		require.Zero(t, f.read(t).StoreCredit)
	})
	t.Run("pack seller mismatch", func(t *testing.T) {
		f.exec(t, `UPDATE store_credit_packs SET seller_id=$2 WHERE id=$1`, f.packID, f.otherSellerID)
		t.Cleanup(func() { f.exec(t, `UPDATE store_credit_packs SET seller_id=$2 WHERE id=$1`, f.packID, f.sellerID) })
		require.Zero(t, f.read(t).StoreCredit)
	})
	t.Run("key routes removed", func(t *testing.T) {
		f.exec(t, `DELETE FROM buyer_key_hops WHERE key_id=$1`, f.keyID)
		t.Cleanup(func() {
			for i, listingID := range f.listingIDs {
				f.exec(t, `INSERT INTO buyer_key_hops(key_id,position,listing_id) VALUES($1,$2,$3)`, f.keyID, i+1, listingID)
			}
		})
		require.Zero(t, f.read(t).StoreCredit)
	})
	t.Run("unpaid credit cash overage blocks activities", func(t *testing.T) {
		debtID := f.id(t, `INSERT INTO sub2api_ops(op_type,target,payload,idempotency_key,status)
		 VALUES('user_balance_subtract','user:101','{}','tm-credit-cash-usage-test','pending') RETURNING id`)
		t.Cleanup(func() { f.exec(t, `DELETE FROM sub2api_ops WHERE id=$1`, debtID) })
		for _, status := range []string{"pending", "dead"} {
			f.exec(t, `UPDATE sub2api_ops SET status=$1 WHERE id=$2`, status, debtID)
			require.Zero(t, f.read(t).StoreCredit, status)
		}
		f.exec(t, `UPDATE sub2api_ops SET status='done' WHERE id=$1`, debtID)
		require.Equal(t, 80.0, f.read(t).StoreCredit)
	})
	t.Run("unrestricted activity models allow any route model", func(t *testing.T) {
		f.exec(t, `UPDATE store_credit_packs SET allowed_models='{}',allowed_platforms='{}' WHERE id=$1`, f.packID)
		t.Cleanup(func() {
			f.exec(t, `UPDATE store_credit_packs SET allowed_models='{match}',allowed_platforms='{anthropic}' WHERE id=$1`, f.packID)
		})
		require.Equal(t, 80.0, f.read(t).StoreCredit)
	})
	t.Run("null upstream whitelist cannot spend unrestricted activity", func(t *testing.T) {
		f.exec(t, `UPDATE store_credit_packs SET allowed_models='{}' WHERE id=$1`, f.packID)
		f.exec(t, `UPDATE upstreams SET model_whitelist=NULL WHERE seller_id=$1`, f.sellerID)
		t.Cleanup(func() {
			f.exec(t, `UPDATE store_credit_packs SET allowed_models='{match}' WHERE id=$1`, f.packID)
			f.exec(t, `UPDATE upstreams SET model_whitelist='{match,other}' WHERE seller_id=$1`, f.sellerID)
		})
		require.Zero(t, f.read(t).StoreCredit, "credit.Select rejects NULL model whitelists for every request")
	})
	t.Run("empty upstream whitelist accepts activity model scope", func(t *testing.T) {
		f.exec(t, `UPDATE upstreams SET model_whitelist='{}' WHERE seller_id=$1`, f.sellerID)
		t.Cleanup(func() {
			f.exec(t, `UPDATE upstreams SET model_whitelist='{match,other}' WHERE seller_id=$1`, f.sellerID)
		})
		require.Equal(t, 80.0, f.read(t).StoreCredit)
	})
	t.Run("usage windows and limits are key scoped", func(t *testing.T) {
		f.exec(t, `UPDATE buyer_keys SET limits='{"quota_usd":100,"rate_limit_5h_usd":10,"rate_limit_1d_usd":20,"rate_limit_7d_usd":30}' WHERE id=$1`, f.keyID)
		t.Cleanup(func() {
			f.exec(t, `DELETE FROM usage_records`)
			f.exec(t, `UPDATE buyer_keys SET limits='{}' WHERE id=$1`, f.keyID)
		})
		f.exec(t, `INSERT INTO usage_records(sub2api_usage_id,created_at,sub2api_account_id,sub2api_user_id,sub2api_api_key_id,
		 model,total_cost,actual_cost,buyer_key_id,buyer_user_id,channel)
		 SELECT id,created_at,1,101,1,'match',cost,cost,key_id,$3,'direct' FROM (VALUES
		 (1,now()-interval '1 hour',1.25,$1::bigint),
		 (2,now()-interval '6 hours',2.5,$1::bigint),
		 (3,now()-interval '30 hours',4,$1::bigint),
		 (4,now()-interval '8 days',8,$1::bigint),
		 (5,now()-interval '1 hour',99,$2::bigint)) x(id,created_at,cost,key_id)`, f.keyID, f.otherKeyID, f.buyerID)
		got := f.read(t)
		require.Equal(t, 15.75, got.Used)
		require.Equal(t, 1.25, got.Used5h)
		require.Equal(t, 3.75, got.Used1d)
		require.Equal(t, 7.75, got.Used7d)
		require.NotNil(t, got.QuotaLimit)
		require.Equal(t, 100.0, *got.QuotaLimit)
		require.NotNil(t, got.RateLimit5h)
		require.Equal(t, 10.0, *got.RateLimit5h)
		require.NotNil(t, got.RateLimit1d)
		require.Equal(t, 20.0, *got.RateLimit1d)
		require.NotNil(t, got.RateLimit7d)
		require.Equal(t, 30.0, *got.RateLimit7d)
	})
	identityRestrictions := []struct {
		name, apply, restore string
		id                   int64
		status               int
	}{
		{"disabled key", `UPDATE buyer_keys SET status='disabled' WHERE id=$1`, `UPDATE buyer_keys SET status='active' WHERE id=$1`, f.keyID, http.StatusForbidden},
		{"deleted key", `UPDATE buyer_keys SET status='deleted' WHERE id=$1`, `UPDATE buyer_keys SET status='active' WHERE id=$1`, f.keyID, http.StatusUnauthorized},
		{"expired key", `UPDATE buyer_keys SET limits='{"expires_at":"2000-01-01T00:00:00Z"}' WHERE id=$1`, `UPDATE buyer_keys SET limits='{}' WHERE id=$1`, f.keyID, http.StatusUnauthorized},
		{"spending blocked key", `UPDATE buyer_keys SET limit_blocked_until=now()+interval '1 hour' WHERE id=$1`, `UPDATE buyer_keys SET limit_blocked_until=NULL WHERE id=$1`, f.keyID, http.StatusTooManyRequests},
		{"frozen buyer", `UPDATE users SET status='frozen' WHERE id=$1`, `UPDATE users SET status='active' WHERE id=$1`, f.buyerID, http.StatusForbidden},
		{"closed buyer", `UPDATE users SET status='closed' WHERE id=$1`, `UPDATE users SET status='active' WHERE id=$1`, f.buyerID, http.StatusForbidden},
		{"locked buyer shadow", `UPDATE shadow_identities SET keys_locked=true WHERE id=$1`, `UPDATE shadow_identities SET keys_locked=false WHERE id=$1`, f.buyerShadowID, http.StatusForbidden},
		{"provisioning buyer shadow", `UPDATE shadow_identities SET status='provisioning' WHERE id=$1`, `UPDATE shadow_identities SET status='active' WHERE id=$1`, f.buyerShadowID, http.StatusServiceUnavailable},
	}
	for _, restriction := range identityRestrictions {
		t.Run(restriction.name, func(t *testing.T) {
			f.exec(t, restriction.apply, restriction.id)
			t.Cleanup(func() { f.exec(t, restriction.restore, restriction.id) })
			_, err := NewDBUsageReader(f.pool).ReadUsage(t.Context(), f.keyID, f.buyerID)
			require.Error(t, err, "database state must override a stale active Redis configuration")
			if restriction.status == http.StatusServiceUnavailable {
				require.ErrorIs(t, err, ErrUsageUnavailable)
			} else {
				var access *usageAccessError
				require.ErrorAs(t, err, &access)
				require.Equal(t, restriction.status, access.status)
			}
		})
	}
	t.Run("wrong owner cannot read another buyer key", func(t *testing.T) {
		_, err := NewDBUsageReader(f.pool).ReadUsage(t.Context(), f.keyID, f.otherBuyerID)
		require.Error(t, err)
	})
	t.Run("missing key is not an empty wallet", func(t *testing.T) {
		_, err := NewDBUsageReader(f.pool).ReadUsage(t.Context(), -1, f.buyerID)
		require.Error(t, err)
	})
	t.Run("missing buyer shadow is unavailable", func(t *testing.T) {
		f.exec(t, `UPDATE shadow_identities SET owner_id=-1 WHERE id=$1`, f.buyerShadowID)
		t.Cleanup(func() { f.exec(t, `UPDATE shadow_identities SET owner_id=$2 WHERE id=$1`, f.buyerShadowID, f.buyerID) })
		_, err := NewDBUsageReader(f.pool).ReadUsage(t.Context(), f.keyID, f.buyerID)
		require.ErrorIs(t, err, ErrUsageUnavailable)
	})
	t.Run("balance reader is valid on a read only connection", func(t *testing.T) {
		config := f.pool.Config().Copy()
		config.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
		readOnly, err := pgxpool.NewWithConfig(t.Context(), config)
		require.NoError(t, err)
		t.Cleanup(readOnly.Close)
		got, err := NewDBUsageReader(readOnly).ReadUsage(t.Context(), f.keyID, f.buyerID)
		require.NoError(t, err, "polling a balance must not mutate wallets, attempts, or usage records")
		require.Equal(t, 25.5, got.Cash)
		require.Equal(t, 80.0, got.StoreCredit)
		require.False(t, got.AsOf.IsZero())
	})
	t.Run("closed pool never returns a cached or zero balance", func(t *testing.T) {
		reader := NewDBUsageReader(f.pool)
		_, err := reader.ReadUsage(t.Context(), f.keyID, f.buyerID)
		require.NoError(t, err)
		f.pool.Close()
		_, err = reader.ReadUsage(context.Background(), f.keyID, f.buyerID)
		require.Error(t, err)
	})
}
