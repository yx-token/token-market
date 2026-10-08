package billing

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ---- 卖家用量 ----
//
// 卖家上游经本平台产生的用量（来自已入账的 usage_records，约有 2 分钟延迟）。只展示用量与收益，不展示买家身份与请求内容。

type UsageTotals struct {
	Requests            int64   `json:"requests"`
	BuyerRequests       int64   `json:"buyer_requests"` // 买家流量（默认分组 + 指定卖家）
	ProbeRequests       int64   `json:"probe_requests"` // 平台检测
	InputTokens         int64   `json:"input_tokens"`
	OutputTokens        int64   `json:"output_tokens"`
	CacheCreationTokens int64   `json:"cache_creation_tokens"`
	CacheReadTokens     int64   `json:"cache_read_tokens"`
	TotalCost           float64 `json:"total_cost"` // 按官方价计的费用
	Payout              float64 `json:"payout"`     // 卖家收益
}

// totalsSQL 生成汇总表达式；t 为表别名（左连接时没有用量的行计为 0）。
func totalsSQL(t string) string {
	p, cnt := "", "count(*)"
	if t != "" {
		p, cnt = t+".", "count("+t+".sub2api_usage_id)"
	}
	return fmt.Sprintf(`%[2]s, %[2]s FILTER (WHERE %[1]schannel IN ('pool', 'direct')), %[2]s FILTER (WHERE %[1]schannel = 'probe'),
	COALESCE(sum(%[1]sinput_tokens), 0), COALESCE(sum(%[1]soutput_tokens), 0), COALESCE(sum(%[1]scache_creation_tokens), 0),
	COALESCE(sum(%[1]scache_read_tokens), 0), COALESCE(sum(%[1]stotal_cost), 0)::float8, COALESCE(sum(%[1]spayout), 0)::float8`, p, cnt)
}

func (t *UsageTotals) dest() []any {
	return []any{&t.Requests, &t.BuyerRequests, &t.ProbeRequests, &t.InputTokens, &t.OutputTokens, &t.CacheCreationTokens, &t.CacheReadTokens,
		&t.TotalCost, &t.Payout}
}

type UsageDay struct {
	Day string `json:"day"`
	UsageTotals
}

type UsageModel struct {
	Model string `json:"model"`
	UsageTotals
}

type UsageUpstream struct {
	UpstreamID int64  `json:"upstream_id"`
	Platform   string `json:"platform"`
	UsageTotals
}

type SellerUsage struct {
	From      time.Time       `json:"from"`
	Days      int             `json:"days"`
	Totals    UsageTotals     `json:"totals"`
	Today     UsageTotals     `json:"today"`
	Daily     []UsageDay      `json:"daily"`
	Models    []UsageModel    `json:"models"`
	Upstreams []UsageUpstream `json:"upstreams"`
}

// SellerUsageSummary 汇总卖家近 days 天的用量；upstreamID 为 0 表示全部上游。
func SellerUsageSummary(ctx context.Context, db DB, sellerID, upstreamID int64, days int) (*SellerUsage, error) {
	out := &SellerUsage{Days: days, Daily: []UsageDay{}, Models: []UsageModel{}, Upstreams: []UsageUpstream{}}
	if err := db.QueryRow(ctx, "SELECT date_trunc('day', now()) - make_interval(days => $1 - 1)", days).Scan(&out.From); err != nil {
		return nil, err
	}
	where := `seller_id = $1 AND ($2 = 0 OR upstream_id = $2) AND created_at >= $3`
	if err := db.QueryRow(ctx, "SELECT "+totalsSQL("")+" FROM usage_records WHERE "+where, sellerID, upstreamID, out.From).
		Scan(out.Totals.dest()...); err != nil {
		return nil, err
	}
	if err := db.QueryRow(ctx, "SELECT "+totalsSQL("")+" FROM usage_records WHERE "+where, sellerID, upstreamID, out.From.AddDate(0, 0, days-1)).
		Scan(out.Today.dest()...); err != nil {
		return nil, err
	}
	// 每日序列补齐没有用量的日子
	rows, err := db.Query(ctx, `WITH d AS (SELECT generate_series($3::timestamptz, date_trunc('day', now()), interval '1 day') AS day)
		SELECT to_char(d.day, 'YYYY-MM-DD'), `+totalsSQL("u")+`
		FROM d LEFT JOIN usage_records u ON date_trunc('day', u.created_at) = d.day AND u.seller_id = $1 AND ($2 = 0 OR u.upstream_id = $2)
		GROUP BY d.day ORDER BY d.day`, sellerID, upstreamID, out.From)
	if err != nil {
		return nil, err
	}
	if out.Daily, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (UsageDay, error) {
		var d UsageDay
		err := row.Scan(append([]any{&d.Day}, d.dest()...)...)
		return d, err
	}); err != nil {
		return nil, err
	}
	rows, err = db.Query(ctx, "SELECT model, "+totalsSQL("")+" FROM usage_records WHERE "+where+" GROUP BY model ORDER BY sum(total_cost) DESC LIMIT 50",
		sellerID, upstreamID, out.From)
	if err != nil {
		return nil, err
	}
	if out.Models, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (UsageModel, error) {
		var m UsageModel
		err := row.Scan(append([]any{&m.Model}, m.dest()...)...)
		return m, err
	}); err != nil {
		return nil, err
	}
	rows, err = db.Query(ctx, `SELECT u.id, u.platform, `+totalsSQL("r")+`
		FROM upstreams u LEFT JOIN usage_records r ON r.upstream_id = u.id AND r.created_at >= $3
		WHERE u.seller_id = $1 AND ($2 = 0 OR u.id = $2) AND u.status <> 'removed'
		GROUP BY u.id, u.platform ORDER BY u.id`, sellerID, upstreamID, out.From)
	if err != nil {
		return nil, err
	}
	out.Upstreams, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (UsageUpstream, error) {
		var u UsageUpstream
		err := row.Scan(append([]any{&u.UpstreamID, &u.Platform}, u.dest()...)...)
		return u, err
	})
	return out, err
}

type UsageRecord struct {
	ID                  int64     `json:"id"`
	CreatedAt           time.Time `json:"created_at"`
	UpstreamID          int64     `json:"upstream_id"`
	ListingID           *int64    `json:"listing_id,omitempty"`
	Channel             string    `json:"channel"`
	Model               string    `json:"model"`
	InputTokens         int       `json:"input_tokens"`
	OutputTokens        int       `json:"output_tokens"`
	CacheCreationTokens int       `json:"cache_creation_tokens"`
	CacheReadTokens     int       `json:"cache_read_tokens"`
	FirstTokenMs        *int      `json:"first_token_ms,omitempty"`
	DurationMs          *int      `json:"duration_ms,omitempty"`
	TotalCost           float64   `json:"total_cost"`
	Payout              float64   `json:"payout"`
}

type UsageRecordFilter struct {
	UpstreamID int64
	Channel    string // buyer / probe / 空
	Model      string
	From, To   time.Time
}

// SellerUsageRecords 返回卖家上游的逐次用量（不含买家信息）。
func SellerUsageRecords(ctx context.Context, db DB, sellerID int64, f UsageRecordFilter, limit, offset int) ([]UsageRecord, error) {
	var from, to *time.Time
	if !f.From.IsZero() {
		from = &f.From
	}
	if !f.To.IsZero() {
		to = &f.To
	}
	rows, err := db.Query(ctx, `SELECT sub2api_usage_id, created_at, upstream_id, listing_id, channel, model, input_tokens, output_tokens,
			cache_creation_tokens, cache_read_tokens, first_token_ms, duration_ms, total_cost::float8, payout::float8
		FROM usage_records
		WHERE seller_id = $1 AND ($2 = 0 OR upstream_id = $2) AND ($3 = '' OR model = $3)
			AND ($4 = '' OR ($4 = 'buyer' AND channel IN ('pool', 'direct')) OR channel = $4)
			AND ($5::timestamptz IS NULL OR created_at >= $5) AND ($6::timestamptz IS NULL OR created_at < $6)
		ORDER BY created_at DESC, sub2api_usage_id DESC LIMIT $7 OFFSET $8`,
		sellerID, f.UpstreamID, f.Model, f.Channel, from, to, limit, offset)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (UsageRecord, error) {
		var r UsageRecord
		err := row.Scan(&r.ID, &r.CreatedAt, &r.UpstreamID, &r.ListingID, &r.Channel, &r.Model, &r.InputTokens, &r.OutputTokens,
			&r.CacheCreationTokens, &r.CacheReadTokens, &r.FirstTokenMs, &r.DurationMs, &r.TotalCost, &r.Payout)
		return r, err
	})
}
