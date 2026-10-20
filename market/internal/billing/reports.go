package billing

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/yx-token/token-market/market/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
)

type UsageItem struct {
	SubAPIUsageID      int64     `json:"usage_id"`
	RequestID          *string   `json:"request_id,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	BuyerKeyID         *int64    `json:"buyer_key_id,omitempty"`
	ListingID          *int64    `json:"listing_id,omitempty"`
	Channel            string    `json:"channel"`
	Model              string    `json:"model"`
	InputTokens        int       `json:"input_tokens"`
	OutputTokens       int       `json:"output_tokens"`
	CacheTokens        int       `json:"cache_tokens"`
	Cost               float64   `json:"cost"`
	CashCost           float64   `json:"cash_cost"`
	CreditPrepaidCNY   float64   `json:"credit_prepaid_cny"`
	CreditCoveredUSD   float64   `json:"credit_covered_usd"`
	CreditCoveredUnits float64   `json:"credit_covered_units"`
	CreditPackID       *int64    `json:"credit_pack_id,omitempty"`
	FirstTokenMs       *int      `json:"first_token_ms,omitempty"`
	DurationMs         *int      `json:"duration_ms,omitempty"`
}

type UsageFilter struct {
	KeyID     int64
	ListingID int64
	Model     string
	From, To  time.Time
}

// BuyerUsage 返回买家的用量明细（来自已入账的 sub2api 用量，几分钟延迟）。
func BuyerUsage(ctx context.Context, db DB, userID int64, f UsageFilter, limit, offset int) ([]UsageItem, error) {
	if f.To.IsZero() {
		f.To = time.Now().Add(time.Hour)
	}
	rows, err := db.Query(ctx, `SELECT sub2api_usage_id, sub2api_request_id, created_at, buyer_key_id, listing_id, channel, model,
			input_tokens, output_tokens, cache_creation_tokens + cache_read_tokens, (COALESCE(cash_cost,actual_cost)+credit_prepaid_cny)::float8, first_token_ms, duration_ms,
			COALESCE(cash_cost,actual_cost)::float8,credit_prepaid_cny::float8,
			COALESCE((SELECT covered_usd FROM store_credit_allocations WHERE usage_id=sub2api_usage_id),0)::float8,
			credit_covered_units::float8,credit_pack_id
		FROM usage_records
		WHERE buyer_user_id = $1 AND created_at >= $2 AND created_at < $3
			AND ($4 = 0 OR buyer_key_id = $4) AND ($5 = 0 OR listing_id = $5) AND ($6 = '' OR model = $6)
		ORDER BY created_at DESC LIMIT $7 OFFSET $8`, userID, f.From, f.To, f.KeyID, f.ListingID, f.Model, limit, offset)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (UsageItem, error) {
		var u UsageItem
		err := row.Scan(&u.SubAPIUsageID, &u.RequestID, &u.CreatedAt, &u.BuyerKeyID, &u.ListingID, &u.Channel, &u.Model,
			&u.InputTokens, &u.OutputTokens, &u.CacheTokens, &u.Cost, &u.FirstTokenMs, &u.DurationMs,
			&u.CashCost, &u.CreditPrepaidCNY, &u.CreditCoveredUSD, &u.CreditCoveredUnits, &u.CreditPackID)
		return u, err
	})
}

type AttemptView struct {
	Hop                int       `json:"hop"`
	ListingID          *int64    `json:"listing_id,omitempty"`
	PoolID             *int64    `json:"pool_id,omitempty"`
	HTTPStatus         *int      `json:"http_status,omitempty"`
	EndReason          string    `json:"end_reason"`
	Committed          bool      `json:"committed"`
	Sub2APIRequestID   *string   `json:"sub2api_request_id,omitempty"`
	Charged            bool      `json:"charged"`
	Cost               float64   `json:"cost"`
	CashCost           float64   `json:"cash_cost"`
	CreditPrepaidCNY   float64   `json:"credit_prepaid_cny"`
	CreditCoveredUSD   float64   `json:"credit_covered_usd"`
	CreditCoveredUnits float64   `json:"credit_covered_units"`
	CreditPackID       *int64    `json:"credit_pack_id,omitempty"`
	Note               string    `json:"note"`
	StartedAt          time.Time `json:"started_at"`
}

// RequestDetail 返回一次买家请求的逐跳明细：每一跳是否产生费用，以 sub2api 记录为准。
// requestID 可以是 Edge 请求 ID（响应头 X-TM-Request-ID），也可以是用量明细中的 sub2api 请求 ID（client:…）。
func RequestDetail(ctx context.Context, db DB, userID int64, requestID string) ([]AttemptView, error) {
	edgeRequestID := requestID
	if sub, ok := strings.CutPrefix(requestID, "client:"); ok {
		err := db.QueryRow(ctx, `SELECT a.edge_request_id FROM edge_attempts a JOIN buyer_keys bk ON bk.id = a.buyer_key_id AND bk.user_id = $1
			WHERE a.sub2api_request_id = $2 LIMIT 1`, userID, sub).Scan(&edgeRequestID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, httpx.NotFound("REQUEST_NOT_FOUND", "request not found")
		}
		if err != nil {
			return nil, err
		}
	}
	rows, err := db.Query(ctx, `SELECT a.hop, a.listing_id, a.pool_id, a.http_status, a.end_reason, a.committed, a.sub2api_request_id,
			ur.sub2api_usage_id IS NOT NULL, COALESCE(COALESCE(ur.cash_cost,ur.actual_cost)+ur.credit_prepaid_cny,0)::float8, a.started_at,
			COALESCE(ur.cash_cost,ur.actual_cost,0)::float8,COALESCE(ur.credit_prepaid_cny,0)::float8,
			COALESCE((SELECT covered_usd FROM store_credit_allocations WHERE usage_id=ur.sub2api_usage_id),0)::float8,
			COALESCE(ur.credit_covered_units,0)::float8,ur.credit_pack_id
		FROM edge_attempts a
		JOIN buyer_keys bk ON bk.id = a.buyer_key_id AND bk.user_id = $1
		LEFT JOIN usage_records ur ON ur.sub2api_request_id = 'client:' || a.sub2api_request_id
		WHERE a.edge_request_id = $2 ORDER BY a.hop`, userID, edgeRequestID)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (AttemptView, error) {
		var a AttemptView
		err := row.Scan(&a.Hop, &a.ListingID, &a.PoolID, &a.HTTPStatus, &a.EndReason, &a.Committed, &a.Sub2APIRequestID, &a.Charged, &a.Cost, &a.StartedAt,
			&a.CashCost, &a.CreditPrepaidCNY, &a.CreditCoveredUSD, &a.CreditCoveredUnits, &a.CreditPackID)
		switch {
		case a.EndReason == "skipped":
			a.Note = "skipped (seller unavailable)"
		case a.Charged && a.EndReason == "client_cancel":
			a.Note = "interrupted, charged by actual usage"
		case a.Charged:
			a.Note = "charged"
		default:
			a.Note = "no charge"
		}
		return a, err
	})
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, httpx.NotFound("REQUEST_NOT_FOUND", "request not found")
	}
	return out, nil
}

type BillDay struct {
	Day                string  `json:"day"`
	Requests           int     `json:"requests"`
	Cost               float64 `json:"cost"`
	CashCost           float64 `json:"cash_cost"`
	CreditPrepaidCNY   float64 `json:"credit_prepaid_cny"`
	CreditCoveredUSD   float64 `json:"credit_covered_usd"`
	CreditCoveredUnits float64 `json:"credit_covered_units"`
}

// BuyerBills 按日汇总买家消费。
func BuyerBills(ctx context.Context, db DB, userID int64, days int) ([]BillDay, error) {
	rows, err := db.Query(ctx, `SELECT to_char(date_trunc('day', created_at), 'YYYY-MM-DD'), count(*), sum(COALESCE(cash_cost,actual_cost)+credit_prepaid_cny)::float8,
			sum(COALESCE(cash_cost,actual_cost))::float8,sum(credit_prepaid_cny)::float8,
			sum(COALESCE((SELECT covered_usd FROM store_credit_allocations WHERE usage_id=sub2api_usage_id),0))::float8,
			sum(credit_covered_units)::float8
		FROM usage_records WHERE buyer_user_id = $1 AND created_at > now() - make_interval(days => $2)
		GROUP BY 1 ORDER BY 1 DESC`, userID, days)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (BillDay, error) {
		var b BillDay
		err := row.Scan(&b.Day, &b.Requests, &b.Cost, &b.CashCost, &b.CreditPrepaidCNY, &b.CreditCoveredUSD, &b.CreditCoveredUnits)
		return b, err
	})
}

type Earnings struct {
	Pending     float64 `json:"pending"`
	Available   float64 `json:"available"`
	Withdrawing float64 `json:"withdrawing"`
	Today       float64 `json:"today"`
	Last7Days   float64 `json:"last_7_days"`
	Total       float64 `json:"total"`
}

func SellerEarnings(ctx context.Context, db DB, sellerID int64) (*Earnings, error) {
	bal, err := Balances(ctx, db, "seller", sellerID)
	if err != nil {
		return nil, err
	}
	e := &Earnings{Pending: bal[KindSellerPending], Available: bal[KindSellerAvailable], Withdrawing: bal[KindSellerWithdrawing]}
	err = db.QueryRow(ctx, `SELECT COALESCE(sum(payout) FILTER (WHERE hour >= date_trunc('day', now())), 0)::float8,
			COALESCE(sum(payout) FILTER (WHERE hour >= now() - interval '7 days'), 0)::float8,
			COALESCE(sum(payout), 0)::float8
		FROM settlement_lines WHERE seller_id = $1`, sellerID).Scan(&e.Today, &e.Last7Days, &e.Total)
	return e, err
}

type SettlementLine struct {
	UpstreamID int64     `json:"upstream_id"`
	Hour       time.Time `json:"hour"`
	Channel    string    `json:"channel"`
	Requests   int       `json:"requests"`
	TotalCost  float64   `json:"official_cost"`
	Payout     float64   `json:"payout"`
	Unfrozen   float64   `json:"unfrozen"`
}

func SellerLines(ctx context.Context, db DB, sellerID, upstreamID int64, limit, offset int) ([]SettlementLine, error) {
	rows, err := db.Query(ctx, `SELECT upstream_id, hour, channel, request_count, total_cost::float8, payout::float8, unfrozen_payout::float8
		FROM settlement_lines WHERE seller_id = $1 AND ($2 = 0 OR upstream_id = $2) ORDER BY hour DESC LIMIT $3 OFFSET $4`,
		sellerID, upstreamID, limit, offset)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (SettlementLine, error) {
		var l SettlementLine
		err := row.Scan(&l.UpstreamID, &l.Hour, &l.Channel, &l.Requests, &l.TotalCost, &l.Payout, &l.Unfrozen)
		return l, err
	})
}

type PlatformSummary struct {
	Cash             float64 `json:"cash"`
	Revenue          float64 `json:"revenue"`
	ProbeCost        float64 `json:"probe_cost"`
	Adjustment       float64 `json:"adjustment"`
	BuyerStoreCredit float64 `json:"buyer_store_credit"`
	BuyerBalances    float64 `json:"buyer_balances"`
	SellerPayable    float64 `json:"seller_payable"`
}

// PlatformLedger 返回平台账面汇总（管理后台）。
func PlatformLedger(ctx context.Context, db DB) (*PlatformSummary, error) {
	bal, err := Balances(ctx, db, "platform", 0)
	if err != nil {
		return nil, err
	}
	s := &PlatformSummary{Cash: bal[KindPlatformCash], Revenue: bal[KindPlatformRevenue], ProbeCost: bal[KindPlatformProbeCost], Adjustment: bal[KindPlatformAdjustment]}
	err = db.QueryRow(ctx, `SELECT
			COALESCE(-sum(e.amount) FILTER (WHERE a.kind IN ('buyer_cash','buyer_store_credit')), 0)::float8,
			COALESCE(-sum(e.amount) FILTER (WHERE a.kind='buyer_store_credit'), 0)::float8,
			COALESCE(-sum(e.amount) FILTER (WHERE a.owner_type = 'seller'), 0)::float8
		FROM ledger_accounts a JOIN ledger_entries e ON e.account_id = a.id WHERE a.owner_type IN ('buyer', 'seller')`).
		Scan(&s.BuyerBalances, &s.BuyerStoreCredit, &s.SellerPayable)
	return s, err
}
