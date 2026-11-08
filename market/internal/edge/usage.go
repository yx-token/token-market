package edge

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yx-token/token-market/market/internal/edgecfg"
	"github.com/yx-token/token-market/market/internal/listing"
)

// UsageSnapshot contains consumption credit in market balance units (CNY).
// Store credit retains its shop/model restrictions and is not withdrawable cash.
type UsageSnapshot struct {
	Cash, QuotaPack, StoreCredit, StoreCreditPrepaidCNY float64
	IsQuotaPack                                         bool
	Used, Used5h, Used1d, Used7d                        float64
	QuotaLimit, RateLimit5h, RateLimit1d, RateLimit7d   *float64
	AsOf                                                time.Time
}

type UsageReader interface {
	ReadUsage(context.Context, int64, int64) (UsageSnapshot, error)
}

var ErrUsageUnavailable = errors.New("usage balance unavailable")

// SetUsageReader is called before serving requests; nil makes balance reads fail closed.
func (s *Server) SetUsageReader(reader UsageReader) { s.usage = reader }

func usagePath(path string) bool {
	return path == "/v1/usage" || path == "/antigravity/v1/usage"
}

type dbUsageReader struct{ db *pgxpool.Pool }

func NewDBUsageReader(db *pgxpool.Pool) UsageReader {
	if db == nil {
		return nil
	}
	return &dbUsageReader{db: db}
}

// This is a balance snapshot, not an upstream request. Keep all aggregates in one
// SQL statement so a concurrent posting cannot mix pre- and post-charge values.
// Pack IDs are distinct: one activity pack can have several keys/listing routes.
const buyerUsageSQL = `SELECT bk.status, buyer_user.status, COALESCE(buyer.status,''),
 COALESCE(buyer.keys_locked,false), (bk.limits->>'expires_at')::timestamptz, bk.limit_blocked_until,
 COALESCE((SELECT -sum(e.amount) FROM ledger_accounts a JOIN ledger_entries e ON e.account_id=a.id
   WHERE a.owner_type='buyer' AND a.owner_id=bk.user_id AND a.kind='buyer_cash'),0)::float8,
 EXISTS(SELECT 1 FROM quota_packs q WHERE q.buyer_key_id=bk.id),
 COALESCE((SELECT sum(q.remaining) FROM quota_packs q
   WHERE q.buyer_key_id=bk.id AND q.buyer_id=bk.user_id AND q.status='active'),0)::float8,
 credit.balance::float8, credit.prepaid::float8,
 spent.total::float8, spent.h5::float8, spent.d1::float8, spent.d7::float8,
 (bk.limits->>'quota_cny')::float8, (bk.limits->>'rate_limit_5h_cny')::float8,
 (bk.limits->>'rate_limit_1d_cny')::float8, (bk.limits->>'rate_limit_7d_cny')::float8, now()
 FROM buyer_keys bk JOIN users buyer_user ON buyer_user.id=bk.user_id
 LEFT JOIN shadow_identities buyer ON buyer.kind='buyer' AND buyer.owner_id=bk.user_id
 CROSS JOIN LATERAL (
   SELECT COALESCE(sum(ur.actual_cost),0) AS total,
     COALESCE(sum(ur.actual_cost) FILTER (WHERE ur.created_at>now()-interval '5 hours'),0) AS h5,
     COALESCE(sum(ur.actual_cost) FILTER (WHERE ur.created_at>now()-interval '1 day'),0) AS d1,
     COALESCE(sum(ur.actual_cost) FILTER (WHERE ur.created_at>now()-interval '7 days'),0) AS d7
   FROM usage_records ur WHERE ur.buyer_key_id=bk.id
 ) spent
 CROSS JOIN LATERAL (
   SELECT COALESCE(sum(p.remaining_balance_units),0) AS balance,
     COALESCE(sum(p.prepaid_remaining_cny),0) AS prepaid
   FROM (
     SELECT DISTINCT p.id,p.remaining_balance_units,p.prepaid_remaining_cny
     FROM store_credit_keys ck JOIN store_credit_packs p ON p.id=ck.pack_id
     JOIN buyer_key_hops bh ON bh.key_id=bk.id AND bh.listing_id=ck.listing_id
     JOIN listings l ON l.id=ck.listing_id AND l.seller_id=p.seller_id
     JOIN upstreams u ON u.id=l.upstream_id AND u.seller_id=p.seller_id
     JOIN buyer_merchants bm ON bm.user_id=bk.user_id AND bm.seller_id=p.seller_id
     JOIN internal_keys ik ON ik.buyer_key_id=bk.id AND ik.sub2api_group_id=l.listing_group_id AND ik.status='active'
     JOIN shadow_identities credit ON credit.kind='credit_pack' AND credit.owner_id=p.id
       AND credit.status='active' AND NOT credit.keys_locked
     JOIN sub2api_ops funding ON funding.id=p.funding_op_id AND funding.status='done'
     WHERE ck.buyer_key_id=bk.id AND p.buyer_id=bk.user_id
       AND ck.status='active' AND ck.sub2api_key_id IS NOT NULL AND ck.sub2api_group_id=l.listing_group_id
       AND p.status='active' AND p.remaining_balance_units>0
       AND buyer.status='active' AND NOT buyer.keys_locked
       AND ` + listing.MarketAvailableSQL + `
       AND (cardinality(p.allowed_platforms)=0 OR u.platform=ANY(p.allowed_platforms))
       AND u.model_whitelist IS NOT NULL
       AND (cardinality(p.allowed_models)=0 OR cardinality(u.model_whitelist)=0 OR p.allowed_models && u.model_whitelist)
       AND NOT EXISTS(SELECT 1 FROM quota_packs q WHERE q.buyer_key_id=bk.id)
       AND NOT EXISTS(SELECT 1 FROM sub2api_ops debt WHERE debt.target='user:'||buyer.sub2api_user_id
         AND debt.idempotency_key LIKE 'tm-credit-cash-%' AND debt.status<>'done')
   ) p
 ) credit
 WHERE bk.id=$1 AND bk.user_id=$2`

type usageAccessError struct {
	status       int
	typ, message string
}

func (e *usageAccessError) Error() string { return e.message }

func (r *dbUsageReader) ReadUsage(ctx context.Context, keyID, userID int64) (UsageSnapshot, error) {
	if r == nil || r.db == nil {
		return UsageSnapshot{}, ErrUsageUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var out UsageSnapshot
	var keyStatus, userStatus, shadowStatus string
	var locked bool
	var expires, blocked *time.Time
	err := r.db.QueryRow(ctx, buyerUsageSQL, keyID, userID).Scan(
		&keyStatus, &userStatus, &shadowStatus, &locked, &expires, &blocked,
		&out.Cash, &out.IsQuotaPack, &out.QuotaPack, &out.StoreCredit, &out.StoreCreditPrepaidCNY,
		&out.Used, &out.Used5h, &out.Used1d, &out.Used7d,
		&out.QuotaLimit, &out.RateLimit5h, &out.RateLimit1d, &out.RateLimit7d, &out.AsOf)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && keyStatus == "deleted" {
		return UsageSnapshot{}, &usageAccessError{http.StatusUnauthorized, "authentication_error", "invalid api key"}
	}
	if err != nil {
		return UsageSnapshot{}, err
	}
	// Redis handles the common checks first. Recheck DB state so a stale route
	// cache cannot expose a freshly revoked key or a wallet locked for refund.
	switch {
	case keyStatus != "active" || userStatus != "active" || locked:
		return UsageSnapshot{}, &usageAccessError{http.StatusForbidden, "permission_error", "api key disabled"}
	case expires != nil && out.AsOf.After(*expires):
		return UsageSnapshot{}, &usageAccessError{http.StatusUnauthorized, "authentication_error", "api key expired"}
	case blocked != nil && out.AsOf.Before(*blocked):
		return UsageSnapshot{}, &usageAccessError{http.StatusTooManyRequests, "rate_limit_error", "spending limit reached"}
	case shadowStatus != "active":
		return UsageSnapshot{}, ErrUsageUnavailable
	}
	return out, nil
}

type usageQuota struct {
	Limit     *float64 `json:"limit,omitempty"`
	Used      float64  `json:"used"`
	Remaining float64  `json:"remaining"`
	Unit      string   `json:"unit"`
}

type usageRateLimit struct {
	Window    string  `json:"window"`
	Limit     float64 `json:"limit"`
	Used      float64 `json:"used"`
	Remaining float64 `json:"remaining"`
	Unit      string  `json:"unit"`
}

type usageResponse struct {
	Remaining          float64          `json:"remaining"`
	Balance            float64          `json:"balance"`
	QuotaRemain        float64          `json:"quota_remaining"`
	WalletRemaining    float64          `json:"wallet_remaining"`
	QuotaPackRemaining float64          `json:"quota_pack_remaining"`
	StoreCredit        float64          `json:"store_credit_remaining_balance_units"`
	StoreCreditCNY     float64          `json:"store_credit_remaining_cny"` // 与 store_credit_remaining_balance_units 相同，按人民币计
	StoreCreditPrepaid float64          `json:"store_credit_prepaid_remaining_cny"`
	Used               float64          `json:"used"`
	Limit              *float64         `json:"limit,omitempty"`
	Quota              *usageQuota      `json:"quota,omitempty"`
	RateLimits         []usageRateLimit `json:"rate_limits,omitempty"`
	Unit               string           `json:"unit"`
	Currency           string           `json:"currency"`
	IsActive           bool             `json:"is_active"`
	IsValid            bool             `json:"isValid"`
	Mode               string           `json:"mode"`
	Status             string           `json:"status"`
	Source             string           `json:"source"`
	Scope              string           `json:"scope"`
	AmountKind         string           `json:"amount_kind"`
	AsOf               time.Time        `json:"as_of"`
}

func (s *Server) serveUsage(w http.ResponseWriter, r *http.Request, cfg *edgecfg.KeyConfig) {
	w.Header().Set("Cache-Control", "no-store")
	if s.usage == nil {
		writeError(w, http.StatusServiceUnavailable, "api_error", ErrUsageUnavailable.Error())
		return
	}
	snapshot, err := s.usage.ReadUsage(r.Context(), cfg.BuyerKeyID, cfg.BuyerUserID)
	if err != nil {
		var access *usageAccessError
		if errors.As(err, &access) {
			writeError(w, access.status, access.typ, access.message)
			return
		}
		s.log.Error("read buyer usage balance", "buyer_key_id", cfg.BuyerKeyID, "err", err)
		writeError(w, http.StatusServiceUnavailable, "api_error", ErrUsageUnavailable.Error())
		return
	}
	wallet, pack, credit, prepaid := snapshot.Cash, 0.0, snapshot.StoreCredit, snapshot.StoreCreditPrepaidCNY
	if snapshot.IsQuotaPack {
		wallet, pack, credit, prepaid = 0, snapshot.QuotaPack, 0, 0
	}
	remaining := math.Max(0, wallet+pack+credit)
	response := usageResponse{WalletRemaining: math.Max(0, wallet), QuotaPackRemaining: math.Max(0, pack),
		StoreCredit: credit, StoreCreditCNY: credit, StoreCreditPrepaid: prepaid, Used: snapshot.Used, Limit: snapshot.QuotaLimit,
		Unit: "CNY", Currency: "CNY", Mode: "unrestricted", Status: "active", Source: "market_ledger",
		Scope: "key_routes", AmountKind: "consumption_credit", AsOf: snapshot.AsOf}
	if response.AsOf.IsZero() {
		response.AsOf = s.now()
	}
	if snapshot.QuotaLimit != nil {
		left := math.Max(0, *snapshot.QuotaLimit-snapshot.Used)
		remaining = math.Min(remaining, left)
		response.Quota = &usageQuota{snapshot.QuotaLimit, snapshot.Used, left, "CNY"}
		response.Mode = "quota_limited"
	}
	for _, window := range []struct {
		name  string
		limit *float64
		used  float64
	}{
		{"5h", snapshot.RateLimit5h, snapshot.Used5h}, {"1d", snapshot.RateLimit1d, snapshot.Used1d}, {"7d", snapshot.RateLimit7d, snapshot.Used7d},
	} {
		if window.limit == nil {
			continue
		}
		left := math.Max(0, *window.limit-window.used)
		remaining = math.Min(remaining, left)
		response.RateLimits = append(response.RateLimits, usageRateLimit{window.name, *window.limit, window.used, left, "CNY"})
		response.Mode = "quota_limited"
	}
	response.Remaining, response.Balance, response.QuotaRemain = remaining, remaining, remaining
	response.IsActive = remaining > 0
	response.IsValid = response.IsActive
	if !response.IsActive {
		response.Status = "balance_exhausted"
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}
