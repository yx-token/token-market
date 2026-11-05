package promo

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
)

type Stats struct {
	PV             int64  `json:"pv"`
	UV             int64  `json:"uv"`
	Signups        int64  `json:"signups"`
	PayingUsers    int64  `json:"paying_users"`
	RechargeAmount string `json:"recharge_amount"`
	BuyerSpend     string `json:"buyer_spend"`
	SellerUsers    int64  `json:"seller_users"`
}

type Organic struct {
	Signups        int64  `json:"signups"`
	PayingUsers    int64  `json:"paying_users"`
	RechargeAmount string `json:"recharge_amount"`
	BuyerSpend     string `json:"buyer_spend"`
	SellerUsers    int64  `json:"seller_users"`
}

func zeroStats() Stats { return Stats{RechargeAmount: "0.00", BuyerSpend: "0.00"} }

type Range struct {
	From  *string
	To    *string
	start *time.Time
	end   *time.Time
}

var beijing = time.FixedZone("Asia/Shanghai", 8*60*60)

func ParseRange(from, to string, daily bool, now time.Time) (Range, error) {
	var out Range
	if daily && to == "" {
		to = now.In(beijing).Format(time.DateOnly)
	}
	var start, end time.Time
	var err error
	if to != "" {
		end, err = time.ParseInLocation(time.DateOnly, to, beijing)
		if err != nil || end.Year() < 1 {
			return out, httpx.BadRequest("INVALID_DATE_RANGE", "dates must use YYYY-MM-DD")
		}
		out.To = &to
		exclusive := end.AddDate(0, 0, 1)
		out.end = &exclusive
	}
	if daily && from == "" {
		from = end.AddDate(0, 0, -29).Format(time.DateOnly)
	}
	if from != "" {
		start, err = time.ParseInLocation(time.DateOnly, from, beijing)
		if err != nil || start.Year() < 1 {
			return out, httpx.BadRequest("INVALID_DATE_RANGE", "dates must use YYYY-MM-DD")
		}
		out.From, out.start = &from, &start
	}
	if out.start != nil && out.end != nil && (start.After(end) || (daily && end.Sub(start) >= 366*24*time.Hour)) {
		return Range{}, httpx.BadRequest("INVALID_DATE_RANGE", "invalid date range or more than 366 days")
	}
	return out, nil
}

type List struct {
	From    *string   `json:"from"`
	To      *string   `json:"to"`
	Items   []Channel `json:"items"`
	Totals  Stats     `json:"totals"`
	Organic Organic   `json:"organic"`
}

// Each event source is filtered by its own event time; cohorts remain lifetime assignments.
// Spend matches billing.BuyerBills: cash paid plus the prepaid credit consumed by the request.
const eventSQL = `WITH cohort AS (
	SELECT u.id, u.created_at, COALESCE(p.channel_id,0) channel_id FROM users u
	LEFT JOIN promo_channel_users p ON p.user_id=u.id WHERE ($3::bigint=0 OR p.channel_id=$3)
), events AS (
	SELECT channel_id,created_at,0::bigint user_id,visitor_id,1 pv,0 signup,0 paying,0::numeric recharge,0::numeric spend,0 seller
	FROM promo_channel_visits WHERE ($3=0 OR channel_id=$3) AND ($1::timestamptz IS NULL OR created_at >= $1) AND ($2::timestamptz IS NULL OR created_at < $2)
	UNION ALL
	SELECT channel_id,created_at,id,NULL,0,1,0,0,0,0 FROM cohort WHERE ($1::timestamptz IS NULL OR created_at >= $1) AND ($2::timestamptz IS NULL OR created_at < $2)
	UNION ALL
	SELECT c.channel_id,COALESCE(r.credited_at,r.paid_at),c.id,NULL,0,0,1,r.amount-r.refunded_amount,0,0
	FROM recharge_orders r JOIN cohort c ON c.id=r.user_id WHERE r.status='credited'
	AND ($1::timestamptz IS NULL OR COALESCE(r.credited_at,r.paid_at) >= $1) AND ($2::timestamptz IS NULL OR COALESCE(r.credited_at,r.paid_at) < $2)
	UNION ALL
	SELECT c.channel_id,r.created_at,c.id,NULL,0,0,0,0,COALESCE(r.cash_cost,r.actual_cost)+r.credit_prepaid_cny,0
	FROM usage_records r JOIN cohort c ON c.id=r.buyer_user_id
	WHERE ($1::timestamptz IS NULL OR r.created_at >= $1) AND ($2::timestamptz IS NULL OR r.created_at < $2)
	UNION ALL
	SELECT c.channel_id,l.created_at,c.id,NULL,0,0,0,0,0,1 FROM listings l JOIN cohort c ON c.id=l.seller_id
	WHERE ($1::timestamptz IS NULL OR l.created_at >= $1) AND ($2::timestamptz IS NULL OR l.created_at < $2)
)
SELECT channel_id, %s, sum(pv),count(DISTINCT visitor_id),sum(signup),count(DISTINCT user_id) FILTER (WHERE paying=1),
	COALESCE(sum(recharge),0)::numeric(30,2)::text,COALESCE(sum(spend),0)::numeric(30,2)::text,
	count(DISTINCT user_id) FILTER (WHERE seller=1) FROM events GROUP BY 1,2`

type metric struct {
	channelID int64
	date      string
	Stats
}

var (
	allStatsSQL   = fmt.Sprintf(eventSQL, `''::text`)
	dailyStatsSQL = fmt.Sprintf(eventSQL, `to_char(created_at AT TIME ZONE 'Asia/Shanghai','YYYY-MM-DD')`)
)

func scanMetric(row pgx.CollectableRow) (metric, error) {
	var m metric
	err := row.Scan(&m.channelID, &m.date, &m.PV, &m.UV, &m.Signups, &m.PayingUsers, &m.RechargeAmount, &m.BuyerSpend, &m.SellerUsers)
	return m, err
}

func addAmount(a, b string) string {
	x, _ := new(big.Rat).SetString(a)
	y, _ := new(big.Rat).SetString(b)
	return x.Add(x, y).FloatString(2)
}

func (s *Stats) add(other Stats) {
	s.PV += other.PV
	s.UV += other.UV
	s.Signups += other.Signups
	s.PayingUsers += other.PayingUsers
	s.SellerUsers += other.SellerUsers
	s.RechargeAmount = addAmount(s.RechargeAmount, other.RechargeAmount)
	s.BuyerSpend = addAmount(s.BuyerSpend, other.BuyerSpend)
}

func (s *Service) List(ctx context.Context, period Range) (*List, error) {
	// Share a snapshot so an attribution committed during this request cannot be counted as organic as well.
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `SELECT `+channelColumns+` FROM promo_channels ORDER BY created_at DESC,id DESC`)
	if err != nil {
		return nil, err
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Channel, error) {
		c, e := scanChannel(row)
		if e != nil {
			return Channel{}, e
		}
		return *c, nil
	})
	if err != nil {
		return nil, err
	}
	rows, err = tx.Query(ctx, allStatsSQL, period.start, period.end, 0)
	if err != nil {
		return nil, err
	}
	metrics, err := pgx.CollectRows(rows, scanMetric)
	if err != nil {
		return nil, err
	}
	byID := map[int64]Stats{}
	for _, m := range metrics {
		byID[m.channelID] = m.Stats
	}
	out := &List{From: period.From, To: period.To, Items: items, Totals: zeroStats()}
	organic := zeroStats()
	if st, ok := byID[0]; ok {
		organic = st
	}
	out.Organic = Organic{organic.Signups, organic.PayingUsers, organic.RechargeAmount, organic.BuyerSpend, organic.SellerUsers}
	for i := range out.Items {
		if st, ok := byID[out.Items[i].ID]; ok {
			out.Items[i].Stats = st
		}
		out.Totals.add(out.Items[i].Stats)
	}
	return out, tx.Commit(ctx)
}

type Day struct {
	Date string `json:"date"`
	Stats
}

func (s *Service) exists(ctx context.Context, id int64) error {
	var exists bool
	if err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM promo_channels WHERE id=$1)`, id).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}

func (s *Service) withStats(ctx context.Context, c *Channel) (*Channel, error) {
	rows, err := s.DB.Query(ctx, allStatsSQL, nil, nil, c.ID)
	if err != nil {
		return nil, err
	}
	metrics, err := pgx.CollectRows(rows, scanMetric)
	if err != nil {
		return nil, err
	}
	if len(metrics) > 0 {
		c.Stats = metrics[0].Stats
	}
	return c, nil
}

func (s *Service) Daily(ctx context.Context, id int64, period Range) ([]Day, error) {
	if err := s.exists(ctx, id); err != nil {
		return nil, err
	}
	rows, err := s.DB.Query(ctx, dailyStatsSQL, period.start, period.end, id)
	if err != nil {
		return nil, err
	}
	metrics, err := pgx.CollectRows(rows, scanMetric)
	if err != nil {
		return nil, err
	}
	byDate := map[string]Stats{}
	for _, m := range metrics {
		byDate[m.date] = m.Stats
	}
	items := make([]Day, 0)
	for d := *period.start; d.Before(*period.end); d = d.AddDate(0, 0, 1) {
		date := d.Format(time.DateOnly)
		st := zeroStats()
		if m, ok := byDate[date]; ok {
			st = m
		}
		items = append(items, Day{date, st})
	}
	return items, nil
}

type User struct {
	UserID         int64     `json:"user_id"`
	Email          string    `json:"email"`
	RegisteredAt   time.Time `json:"registered_at"`
	RechargeAmount string    `json:"recharge_amount"`
	BuyerSpend     string    `json:"buyer_spend"`
	IsSeller       bool      `json:"is_seller"`
}

type Users struct {
	Items []User `json:"items"`
	Total int64  `json:"total"`
}

func (s *Service) Users(ctx context.Context, id int64, limit, offset int) (*Users, error) {
	if err := s.exists(ctx, id); err != nil {
		return nil, err
	}
	out := &Users{}
	if err := s.DB.QueryRow(ctx, `SELECT count(*) FROM promo_channel_users WHERE channel_id=$1`, id).Scan(&out.Total); err != nil {
		return nil, err
	}
	rows, err := s.DB.Query(ctx, `SELECT u.id,u.email,u.created_at,
		COALESCE((SELECT sum(amount-refunded_amount) FROM recharge_orders WHERE user_id=u.id AND status='credited'),0)::numeric(30,2)::text,
		COALESCE((SELECT sum(COALESCE(cash_cost,actual_cost)+credit_prepaid_cny) FROM usage_records WHERE buyer_user_id=u.id),0)::numeric(30,2)::text,
		EXISTS(SELECT 1 FROM listings WHERE seller_id=u.id)
		FROM promo_channel_users p JOIN users u ON u.id=p.user_id WHERE p.channel_id=$1 ORDER BY u.created_at DESC,u.id DESC LIMIT $2 OFFSET $3`, id, limit, offset)
	if err != nil {
		return nil, err
	}
	out.Items, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (User, error) {
		var u User
		err := row.Scan(&u.UserID, &u.Email, &u.RegisteredAt, &u.RechargeAmount, &u.BuyerSpend, &u.IsSeller)
		return u, err
	})
	return out, err
}
