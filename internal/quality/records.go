package quality

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

// RecordFilter 是检测记录与按题统计的筛选条件，空值表示不限。
type RecordFilter struct {
	Platform   string
	UpstreamID int64
	Model      string
	Trigger    string // 为空时不含基线对照检测；reference 只看基线对照检测
	Status     string
	Verdict    string
	From, To   *time.Time
}

// RunRecord 是全局检测记录中的一条：检测本身、上游信息与各套件的题数和通过数。
type RunRecord struct {
	RunSummary
	Platform     string                `json:"platform"`
	AuthType     string                `json:"auth_type"`
	SellerID     int64                 `json:"seller_id"`
	AccountEmail *string               `json:"account_email,omitempty"`
	IsReference  bool                  `json:"is_reference"`
	Suites       map[string]SuiteCount `json:"suites"`
}

type SuiteCount struct {
	Items  int `json:"items"`
	Passed int `json:"passed"`
}

// recordWhere 拼接筛选条件，参数从 $1 开始，依次为 RecordFilter 的各字段。
const recordWhere = `($1 = '' OR u.platform = $1) AND ($2 = 0 OR r.upstream_id = $2) AND ($3 = '' OR r.model = $3)
	AND (CASE WHEN $4 = '' THEN r.trigger <> 'reference' ELSE r.trigger = $4 END)
	AND ($5 = '' OR r.status = $5) AND ($6 = '' OR r.summary->>'verdict' = $6)
	AND ($7::timestamptz IS NULL OR r.queued_at >= $7) AND ($8::timestamptz IS NULL OR r.queued_at < $8)`

func (f RecordFilter) args() []any {
	return []any{f.Platform, f.UpstreamID, f.Model, f.Trigger, f.Status, f.Verdict, f.From, f.To}
}

// ListRunRecords 返回全部上游的检测记录（新的在前）。
func ListRunRecords(ctx context.Context, db DB, f RecordFilter, limit, offset int) ([]RunRecord, error) {
	rows, err := db.Query(ctx, "SELECT "+runSummaryColumns+`, u.platform, u.auth_type, u.seller_id, u.account_email, u.is_reference,
			COALESCE((SELECT jsonb_object_agg(s.suite, jsonb_build_object('items', s.n, 'passed', s.p))
				FROM (SELECT x.suite, count(*) AS n, count(*) FILTER (WHERE x.passed) AS p
					FROM probe_results x WHERE x.run_id = r.id GROUP BY x.suite) s), '{}')
		FROM probe_runs r JOIN upstreams u ON u.id = r.upstream_id
		WHERE `+recordWhere+` ORDER BY r.id DESC LIMIT $9 OFFSET $10`, append(f.args(), limit, offset)...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (RunRecord, error) {
		var rec RunRecord
		var summary, suites []byte
		s := &rec.RunSummary
		err := row.Scan(&s.ID, &s.UpstreamID, &s.BatchID, &s.Trigger, &s.Model, &s.Status, &summary, &s.Items, &s.Passed, &s.QueuedAt, &s.FinishedAt,
			&rec.Platform, &rec.AuthType, &rec.SellerID, &rec.AccountEmail, &rec.IsReference, &suites)
		if err != nil {
			return rec, err
		}
		if len(summary) > 0 {
			s.Summary = summary
		}
		return rec, json.Unmarshal(suites, &rec.Suites)
	})
}

// ItemStat 是一道题在筛选范围内的答题统计；被测上游与基线账号分开统计。
type ItemStat struct {
	ItemID             int64      `json:"item_id"`
	Suite              string     `json:"suite"`
	Category           string     `json:"category"`
	Prompt             string     `json:"prompt"`
	Models             []string   `json:"models"`
	Status             string     `json:"status"`
	Asked              int        `json:"asked"`
	Passed             int        `json:"passed"`
	Errors             int        `json:"errors"`
	AvgDurationMs      float64    `json:"avg_duration_ms"`
	AvgOutputTokens    float64    `json:"avg_output_tokens"`
	AvgReasoningTokens float64    `json:"avg_reasoning_tokens"`
	LastAskedAt        *time.Time `json:"last_asked_at,omitempty"`
	RefAsked           int        `json:"ref_asked"`
	RefPassed          int        `json:"ref_passed"`
}

// ListItemStats 按题统计答题情况。筛选条件作用于被测上游的检测；基线对照检测只按厂商、模型与时间筛选，单独统计。
func ListItemStats(ctx context.Context, db DB, f RecordFilter) ([]ItemStat, error) {
	rows, err := db.Query(ctx, `WITH res AS (
			SELECT pr.*, (r.trigger = 'reference') AS ref FROM probe_results pr
			JOIN probe_runs r ON r.id = pr.run_id JOIN upstreams u ON u.id = r.upstream_id
			WHERE pr.item_id IS NOT NULL AND (`+recordWhere+` OR (r.trigger = 'reference' AND ($1 = '' OR u.platform = $1)
				AND ($3 = '' OR r.model = $3) AND ($7::timestamptz IS NULL OR r.queued_at >= $7) AND ($8::timestamptz IS NULL OR r.queued_at < $8)))
		)
		SELECT i.id, i.suite, i.category, i.prompt, i.models, i.status,
			count(*) FILTER (WHERE NOT res.ref), count(*) FILTER (WHERE NOT res.ref AND res.passed),
			count(*) FILTER (WHERE NOT res.ref AND res.error_class IS NOT NULL),
			COALESCE(avg(res.duration_ms) FILTER (WHERE NOT res.ref AND res.error_class IS NULL), 0)::float8,
			COALESCE(avg(res.output_tokens) FILTER (WHERE NOT res.ref AND res.error_class IS NULL), 0)::float8,
			COALESCE(avg(res.reasoning_tokens) FILTER (WHERE NOT res.ref AND res.error_class IS NULL), 0)::float8,
			max(res.created_at) FILTER (WHERE NOT res.ref),
			count(*) FILTER (WHERE res.ref), count(*) FILTER (WHERE res.ref AND res.passed)
		FROM res JOIN probe_items i ON i.id = res.item_id
		GROUP BY i.id ORDER BY i.id`, f.args()...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (ItemStat, error) {
		var s ItemStat
		err := row.Scan(&s.ItemID, &s.Suite, &s.Category, &s.Prompt, &s.Models, &s.Status, &s.Asked, &s.Passed, &s.Errors,
			&s.AvgDurationMs, &s.AvgOutputTokens, &s.AvgReasoningTokens, &s.LastAskedAt, &s.RefAsked, &s.RefPassed)
		return s, err
	})
}

// ItemResult 是一道题在某次检测中的结果，用于查看这道题在各上游上的表现。
type ItemResult struct {
	RunID           int64     `json:"run_id"`
	UpstreamID      int64     `json:"upstream_id"`
	Platform        string    `json:"platform"`
	AccountEmail    *string   `json:"account_email,omitempty"`
	Model           string    `json:"model"`
	Trigger         string    `json:"trigger"`
	Passed          *bool     `json:"passed"`
	HTTPStatus      *int      `json:"http_status,omitempty"`
	ErrorClass      *string   `json:"error_class,omitempty"`
	TTFTMs          int       `json:"ttft_ms"`
	DurationMs      int       `json:"duration_ms"`
	OutputTokens    int       `json:"output_tokens"`
	ReasoningTokens int       `json:"reasoning_tokens"`
	ResponseModel   *string   `json:"response_model,omitempty"`
	Excerpt         *string   `json:"excerpt,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

// ListItemResults 返回一道题最近的答题结果（新的在前），含基线对照检测。
func ListItemResults(ctx context.Context, db DB, itemID int64, f RecordFilter, limit int) ([]ItemResult, error) {
	rows, err := db.Query(ctx, `SELECT r.id, r.upstream_id, u.platform, u.account_email, r.model, r.trigger, pr.passed, pr.http_status, pr.error_class,
			COALESCE(pr.ttft_ms, 0), COALESCE(pr.duration_ms, 0), COALESCE(pr.output_tokens, 0), COALESCE(pr.reasoning_tokens, 0),
			pr.response_model, pr.raw_excerpt, pr.created_at
		FROM probe_results pr JOIN probe_runs r ON r.id = pr.run_id JOIN upstreams u ON u.id = r.upstream_id
		WHERE pr.item_id = $1 AND ($2 = '' OR u.platform = $2) AND ($3 = 0 OR r.upstream_id = $3) AND ($4 = '' OR r.model = $4)
			AND ($5::timestamptz IS NULL OR r.queued_at >= $5) AND ($6::timestamptz IS NULL OR r.queued_at < $6)
		ORDER BY pr.id DESC LIMIT $7`, itemID, f.Platform, f.UpstreamID, f.Model, f.From, f.To, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (ItemResult, error) {
		var r ItemResult
		err := row.Scan(&r.RunID, &r.UpstreamID, &r.Platform, &r.AccountEmail, &r.Model, &r.Trigger, &r.Passed, &r.HTTPStatus, &r.ErrorClass,
			&r.TTFTMs, &r.DurationMs, &r.OutputTokens, &r.ReasoningTokens, &r.ResponseModel, &r.Excerpt, &r.CreatedAt)
		return r, err
	})
}
