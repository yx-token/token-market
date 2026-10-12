package quality

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/DueGin/token-market/internal/inspection"
	"github.com/DueGin/token-market/internal/listing"
	"github.com/DueGin/token-market/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
)

type Report struct {
	ID         int64           `json:"id"`
	UpstreamID int64           `json:"upstream_id"`
	Trigger    string          `json:"trigger"`
	Verdict    string          `json:"verdict"`
	Summary    json.RawMessage `json:"summary"`
	Impact     json.RawMessage `json:"impact"`
	CreatedAt  time.Time       `json:"created_at"`
}

const reportColumns = `id, upstream_id, trigger, verdict, summary, impact, created_at`

func scanReport(row pgx.CollectableRow) (Report, error) {
	var r Report
	err := row.Scan(&r.ID, &r.UpstreamID, &r.Trigger, &r.Verdict, &r.Summary, &r.Impact, &r.CreatedAt)
	if err == nil {
		r.Summary = publicReportSummary(r.Summary)
	}
	return r, err
}

// SellerReports 返回卖家某个上游的测试报告（已脱敏，不含题目与基线信息）。
func SellerReports(ctx context.Context, db DB, sellerID, upstreamID int64, limit, offset int) ([]Report, error) {
	if _, err := listing.GetSellerUpstream(ctx, db, sellerID, upstreamID); err != nil {
		return nil, err
	}
	rows, err := db.Query(ctx, "SELECT "+reportColumns+" FROM test_reports WHERE upstream_id = $1 ORDER BY id DESC LIMIT $2 OFFSET $3",
		upstreamID, limit, offset)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanReport)
}

func SellerReport(ctx context.Context, db DB, sellerID, reportID int64) (*Report, error) {
	rows, err := db.Query(ctx, "SELECT "+reportColumns+` FROM test_reports
		WHERE id = $1 AND upstream_id IN (SELECT id FROM upstreams WHERE seller_id = $2)`, reportID, sellerID)
	if err != nil {
		return nil, err
	}
	reports, err := pgx.CollectRows(rows, scanReport)
	if err != nil {
		return nil, err
	}
	if len(reports) == 0 {
		return nil, httpx.NotFound("REPORT_NOT_FOUND", "report not found")
	}
	return &reports[0], nil
}

type TrendPoint struct {
	Day           string   `json:"day"`
	RelativeScore *float64 `json:"relative_score,omitempty"`
	Checks        int      `json:"checks"`
	Degraded      int      `json:"degraded"`
	Unavailable   int      `json:"unavailable"`
}

// QualityTrend 返回近 N 天每日的智力相对分与异常次数。
func QualityTrend(ctx context.Context, db DB, sellerID, upstreamID int64, days int) ([]TrendPoint, error) {
	if _, err := listing.GetSellerUpstream(ctx, db, sellerID, upstreamID); err != nil {
		return nil, err
	}
	rows, err := db.Query(ctx, `SELECT to_char(date_trunc('day', created_at), 'YYYY-MM-DD'), avg(relative_score), count(*),
			count(*) FILTER (WHERE verdict = 'degraded'), count(*) FILTER (WHERE verdict = 'unavailable')
		FROM quality_verdicts WHERE upstream_id = $1 AND created_at > now() - make_interval(days => $2)
		GROUP BY 1 ORDER BY 1`, upstreamID, days)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (TrendPoint, error) {
		var p TrendPoint
		err := row.Scan(&p.Day, &p.RelativeScore, &p.Checks, &p.Degraded, &p.Unavailable)
		return p, err
	})
}

// ManualRetest 卖家手动复测：仅在该厂商开启检测时可用，每天限次。
func (e *Engine) ManualRetest(ctx context.Context, sellerID, upstreamID int64) (int64, error) {
	u, err := listing.GetSellerUpstream(ctx, e.DB, sellerID, upstreamID)
	if err != nil {
		return 0, err
	}
	if u.Sub2APIAccountID == nil || (u.Status != listing.StatusReady && u.Status != listing.StatusListed) {
		return 0, httpx.Conflict("NOT_TESTABLE", "upstream is not ready for testing")
	}
	p, err := EffectivePolicy(ctx, e.DB, u.ID, u.Platform, u.AuthType)
	if err != nil {
		return 0, err
	}
	if !p.Enabled {
		return 0, httpx.Forbidden("DETECTION_DISABLED", "detection is disabled for this vendor")
	}
	st, err := e.Settings(ctx)
	if err != nil {
		return 0, err
	}
	var used int
	if err := e.DB.QueryRow(ctx, `INSERT INTO upstream_quality_state (upstream_id, manual_runs_today, manual_runs_date) VALUES ($1, 0, current_date)
		ON CONFLICT (upstream_id) DO UPDATE SET
			manual_runs_today = CASE WHEN upstream_quality_state.manual_runs_date = current_date THEN upstream_quality_state.manual_runs_today ELSE 0 END,
			manual_runs_date = current_date
		RETURNING manual_runs_today`, u.ID).Scan(&used); err != nil {
		return 0, err
	}
	if used >= st.ManualRetestDailyLimit {
		return 0, httpx.TooMany("RETEST_LIMIT", "daily manual retest limit reached")
	}
	id, err := e.QueueRun(ctx, u.ID, "seller_manual")
	if err != nil {
		return 0, err
	}
	_, err = e.DB.Exec(ctx, "UPDATE upstream_quality_state SET manual_runs_today = manual_runs_today + 1 WHERE upstream_id = $1", u.ID)
	return id, err
}

type Appeal struct {
	ID         int64      `json:"id"`
	ReportID   int64      `json:"report_id"`
	SellerID   int64      `json:"seller_id"`
	Reason     string     `json:"reason"`
	Status     string     `json:"status"`
	AdminNote  *string    `json:"admin_note,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
}

const appealColumns = `id, report_id, seller_id, reason, status, admin_note, created_at, resolved_at`

func scanAppeal(row pgx.Row) (*Appeal, error) {
	var a Appeal
	if err := row.Scan(&a.ID, &a.ReportID, &a.SellerID, &a.Reason, &a.Status, &a.AdminNote, &a.CreatedAt, &a.ResolvedAt); err != nil {
		return nil, err
	}
	return &a, nil
}

// CreateAppeal 卖家对降智、不可用或正式扣价项提出申诉。
func CreateAppeal(ctx context.Context, db DB, sellerID, reportID int64, reason string) (*Appeal, error) {
	r, err := SellerReport(ctx, db, sellerID, reportID)
	if err != nil {
		return nil, err
	}
	var summary struct {
		Checks map[string]inspection.Check `json:"checks"`
	}
	if err := json.Unmarshal(r.Summary, &summary); err != nil {
		return nil, err
	}
	if r.Verdict != "degraded" && r.Verdict != "unavailable" && !hasAppliedDiscount(summary.Checks) {
		return nil, httpx.BadRequest("NOT_APPEALABLE", "只有降智、不可用或正式扣价项可以申诉")
	}
	if reason == "" {
		return nil, httpx.BadRequest("REASON_REQUIRED", "reason is required")
	}
	var open int
	if err := db.QueryRow(ctx, "SELECT count(*) FROM appeals WHERE report_id = $1 AND status IN ('open', 'retesting')", reportID).Scan(&open); err != nil {
		return nil, err
	}
	if open > 0 {
		return nil, httpx.Conflict("APPEAL_EXISTS", "an appeal for this report is already open")
	}
	return scanAppeal(db.QueryRow(ctx, `INSERT INTO appeals (report_id, seller_id, reason) VALUES ($1, $2, $3) RETURNING `+appealColumns,
		reportID, sellerID, reason))
}

func ListAppeals(ctx context.Context, db DB, status string, limit, offset int) ([]Appeal, error) {
	rows, err := db.Query(ctx, "SELECT "+appealColumns+" FROM appeals WHERE $1 = '' OR status = $1 ORDER BY id DESC LIMIT $2 OFFSET $3", status, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Appeal
	for rows.Next() {
		a, err := scanAppeal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// ResolveAppeal 管理员处理申诉：retest 用三倍题量复测（不受预算限制），overturn 直接恢复健康，uphold 维持判定。
func (e *Engine) ResolveAppeal(ctx context.Context, appealID int64, action, note string) (*Appeal, error) {
	a, err := scanAppeal(e.DB.QueryRow(ctx, "SELECT "+appealColumns+" FROM appeals WHERE id = $1", appealID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.NotFound("APPEAL_NOT_FOUND", "appeal not found")
	}
	if err != nil {
		return nil, err
	}
	if a.Status != "open" {
		return nil, httpx.Conflict("APPEAL_NOT_OPEN", "appeal is not open")
	}
	var upstreamID int64
	if err := e.DB.QueryRow(ctx, "SELECT upstream_id FROM test_reports WHERE id = $1", a.ReportID).Scan(&upstreamID); err != nil {
		return nil, err
	}
	switch action {
	case "retest":
		if _, err := e.QueueRun(ctx, upstreamID, "appeal"); err != nil {
			return nil, err
		}
		_, err = e.DB.Exec(ctx, "UPDATE appeals SET status = 'retesting', admin_note = NULLIF($2, '') WHERE id = $1", appealID, note)
	case "overturn":
		if _, err := listing.SetHealth(ctx, e.DB, upstreamID, listing.HealthHealthy, ""); err != nil {
			return nil, err
		}
		_, err = e.DB.Exec(ctx, "UPDATE appeals SET status = 'overturned', admin_note = NULLIF($2, ''), resolved_at = now() WHERE id = $1", appealID, note)
		e.kick(ctx)
	case "uphold":
		_, err = e.DB.Exec(ctx, "UPDATE appeals SET status = 'upheld', admin_note = NULLIF($2, ''), resolved_at = now() WHERE id = $1", appealID, note)
	default:
		return nil, httpx.BadRequest("INVALID_ACTION", "action must be retest, overturn or uphold")
	}
	if err != nil {
		return nil, err
	}
	return scanAppeal(e.DB.QueryRow(ctx, "SELECT "+appealColumns+" FROM appeals WHERE id = $1", appealID))
}

type OverviewRow struct {
	Platform string         `json:"platform"`
	Health   map[string]int `json:"health"`
	Grades   map[string]int `json:"grades"`
}

type Overview struct {
	Platforms         []OverviewRow     `json:"platforms"`
	VendorWide        []json.RawMessage `json:"recent_vendor_wide_events"`
	DegradedUpstreams []int64           `json:"degraded_upstreams"`
}

// AdminOverview 管理后台的质量看板。
func AdminOverview(ctx context.Context, db DB) (*Overview, error) {
	out := &Overview{}
	rows, err := db.Query(ctx, `SELECT platform, health, COALESCE(grade, '-'), count(*) FROM upstreams
		WHERE status IN ('ready', 'listed') AND NOT is_reference GROUP BY 1, 2, 3 ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	byPlatform := map[string]*OverviewRow{}
	var order []string
	for rows.Next() {
		var p, h, g string
		var n int
		if err := rows.Scan(&p, &h, &g, &n); err != nil {
			rows.Close()
			return nil, err
		}
		r, ok := byPlatform[p]
		if !ok {
			r = &OverviewRow{Platform: p, Health: map[string]int{}, Grades: map[string]int{}}
			byPlatform[p] = r
			order = append(order, p)
		}
		r.Health[h] += n
		r.Grades[g] += n
	}
	rows.Close()
	for _, p := range order {
		out.Platforms = append(out.Platforms, *byPlatform[p])
	}
	rows, err = db.Query(ctx, `SELECT jsonb_build_object('upstream_id', upstream_id, 'model', model, 'at', created_at)
		FROM quality_verdicts WHERE attribution = 'vendor_wide' AND created_at > now() - interval '7 days' ORDER BY id DESC LIMIT 50`)
	if err != nil {
		return nil, err
	}
	if out.VendorWide, err = pgx.CollectRows(rows, pgx.RowTo[json.RawMessage]); err != nil {
		return nil, err
	}
	rows, err = db.Query(ctx, `SELECT id FROM upstreams WHERE health = 'degraded' AND status IN ('ready', 'listed') ORDER BY id`)
	if err != nil {
		return nil, err
	}
	out.DegradedUpstreams, err = pgx.CollectRows(rows, pgx.RowTo[int64])
	return out, err
}

// SeedDefaultItems 仅空库写入旧基础题，另按稳定键补缺影子题，不覆盖管理员维护。
func SeedDefaultItems(ctx context.Context, db DB) (int, error) {
	var n int
	if err := db.QueryRow(ctx, "SELECT count(*) FROM probe_items").Scan(&n); err != nil {
		return 0, err
	}
	if n > 0 {
		return SyncInspectionItems(ctx, db)
	}
	items := defaultItems()
	for _, it := range items {
		if _, err := CreateItem(ctx, db, it); err != nil {
			return 0, err
		}
	}
	added, err := SyncInspectionItems(ctx, db)
	return len(items) + added, err
}

// defaultItems 是内置的基础题。
func defaultItems() []Item {
	num := func(v float64) *float64 { return &v }
	high := "high"
	items := []Item{
		{Suite: "iq", Category: "arithmetic", Prompt: "Compute 37 × 43. Reply with only the number.", Grader: Grader{Type: "numeric", Number: num(1591)}},
		{Suite: "iq", Category: "arithmetic", Prompt: "What is 2^20? Reply with only the number.", Grader: Grader{Type: "numeric", Number: num(1048576)}},
		{Suite: "iq", Category: "word_problem", Prompt: "A train travels 180 km in 2.5 hours, then 120 km in 1.5 hours. What is its average speed in km/h for the whole trip? Reply with only the number.", Grader: Grader{Type: "numeric", Number: num(75)}},
		{Suite: "iq", Category: "logic", Prompt: "If all bloops are razzies and all razzies are lazzies, are all bloops definitely lazzies? Answer only Yes or No.", Grader: Grader{Type: "regex", Pattern: `(?i)^\W*yes\b`}},
		{Suite: "iq", Category: "instruction", Prompt: "Sort these numbers in ascending order and reply with them comma-separated with no spaces: 42,7,19,3,88,23", Grader: Grader{Type: "exact", Answer: "3,7,19,23,42,88"}},
		{Suite: "iq", Category: "knowledge", Prompt: "How many prime numbers are there between 1 and 100? Reply with only the number.", Grader: Grader{Type: "numeric", Number: num(25)}},
		{Suite: "iq", Category: "string", Prompt: "Reverse the string 'tokenmarket' and reply with only the result.", Grader: Grader{Type: "exact", Answer: "tekramnekot", CaseInsensitive: true}},
		{Suite: "iq", Category: "arithmetic", Prompt: "What is the sum of the digits of 987654321? Reply with only the number.", Grader: Grader{Type: "numeric", Number: num(45)}},
		{Suite: "iq", Category: "logic", Prompt: "Alice is older than Bob. Bob is older than Carol. Who is the youngest? Answer with one name.", Grader: Grader{Type: "contains", Answer: "carol", CaseInsensitive: true}},
		{Suite: "iq", Category: "algebra", Prompt: "Solve for x: 3x + 7 = 31. Reply with only the number.", Grader: Grader{Type: "numeric", Number: num(8)}},
		{Suite: "iq", Category: "format", Prompt: "Return a JSON object with keys \"city\" and \"country\" describing Paris. Return only the JSON.", Grader: Grader{Type: "json_keys", Keys: []string{"city", "country"}}},
		{Suite: "iq", Category: "string", Prompt: "How many times does the letter 'r' appear in the word 'strawberry'? Reply with only the number.", Grader: Grader{Type: "numeric", Number: num(3)}},
		{Suite: "iq", Category: "arithmetic", Prompt: "What is 15% of 240? Reply with only the number.", Grader: Grader{Type: "numeric", Number: num(36)}},
		{Suite: "iq", Category: "choice", Prompt: "Which planet is closest to the sun? A) Venus B) Mercury C) Earth D) Mars. Reply with only the letter.", Grader: Grader{Type: "choice", Answer: "B"}},
		{Suite: "reasoning", Category: "number_theory", Prompt: "Find the smallest positive integer n such that n! is divisible by 2^10. Reply with only the number.", Grader: Grader{Type: "numeric", Number: num(12)}, ReasoningEffort: &high, MaxTokens: 4096},
		{Suite: "reasoning", Category: "combinatorics", Prompt: "How many ways can you choose 3 items from 10 distinct items, if two specific items cannot both be chosen? Reply with only the number.", Grader: Grader{Type: "numeric", Number: num(112)}, ReasoningEffort: &high, MaxTokens: 4096},
		{Suite: "reasoning", Category: "modular", Prompt: "What is the remainder when 2^100 is divided by 7? Reply with only the number.", Grader: Grader{Type: "numeric", Number: num(2)}, ReasoningEffort: &high, MaxTokens: 4096},
		{Suite: "reasoning", Category: "trap", Prompt: "A bat and a ball cost $1.10 in total. The bat costs $1.00 more than the ball. How many cents does the ball cost? Reply with only the number.", Grader: Grader{Type: "numeric", Number: num(5)}, ReasoningEffort: &high, MaxTokens: 4096},
		{Suite: "reasoning", Category: "algebra", Prompt: "The sum of three consecutive even integers is 78. What is the largest of them? Reply with only the number.", Grader: Grader{Type: "numeric", Number: num(28)}, ReasoningEffort: &high, MaxTokens: 4096},
		{Suite: "long_context", Category: "needle", Prompt: "{{HAYSTACK:20000}}\n\nWhat is the secret code mentioned in the text above? Reply with only the code.", Grader: Grader{Type: "contains", Answer: "{{NEEDLE}}"}, MaxTokens: 64},
		{Suite: "long_context", Category: "needle", Prompt: "{{HAYSTACK:80000}}\n\nWhat is the secret code mentioned in the text above? Reply with only the code.", Grader: Grader{Type: "contains", Answer: "{{NEEDLE}}"}, MaxTokens: 64},
	}
	for i := range items {
		if items[i].MaxTokens == 0 {
			items[i].MaxTokens = 512
		}
	}
	return items
}

// RunSummary 是管理员查看的一次检测记录（含基线对照检测）。
type RunSummary struct {
	ID         int64           `json:"id"`
	UpstreamID int64           `json:"upstream_id"`
	BatchID    string          `json:"batch_id"`
	Trigger    string          `json:"trigger"`
	Model      string          `json:"model"`
	Status     string          `json:"status"`
	Summary    json.RawMessage `json:"summary,omitempty"`
	Items      int             `json:"items"`
	Passed     int             `json:"passed"`
	QueuedAt   time.Time       `json:"queued_at"`
	FinishedAt *time.Time      `json:"finished_at,omitempty"`
}

// RunResult 是一次检测中一道题的结果，包含题目与回答片段，仅管理员可见。
type RunResult struct {
	CheckKey            string   `json:"check_key"`
	InstanceIdx         int      `json:"instance_idx"`
	Step                int      `json:"step"`
	InstanceHash        string   `json:"instance_hash"`
	GeneratorVersion    string   `json:"generator_version"`
	InputTokens         *int64   `json:"input_tokens"`
	CacheReadTokens     *int64   `json:"cache_read_tokens"`
	CacheCreationTokens *int64   `json:"cache_creation_tokens"`
	StopReason          string   `json:"stop_reason"`
	Effort              string   `json:"effort"`
	Flags               []string `json:"flags"`
	ErrorBody           string   `json:"error_body"`

	ItemID          *int64   `json:"item_id,omitempty"`
	Suite           string   `json:"suite"`
	Category        string   `json:"category"`
	Prompt          string   `json:"prompt"`
	Models          []string `json:"models"`
	Passed          *bool    `json:"passed"`
	HTTPStatus      *int     `json:"http_status,omitempty"`
	ErrorClass      *string  `json:"error_class,omitempty"`
	TTFTMs          int      `json:"ttft_ms"`
	DurationMs      int      `json:"duration_ms"`
	OutputTokens    int      `json:"output_tokens"`
	ReasoningTokens int      `json:"reasoning_tokens"`
	ResponseModel   *string  `json:"response_model,omitempty"`
	Excerpt         *string  `json:"excerpt,omitempty"`
}

// RunDetail 是一次检测的逐题明细；有基线对照时一并返回基线账号同批次的检测。
type RunDetail struct {
	Run        RunSummary  `json:"run"`
	Results    []RunResult `json:"results"`
	Reference  *RunSummary `json:"reference,omitempty"`
	RefResults []RunResult `json:"reference_results,omitempty"`
}

const runSummaryColumns = `r.id, r.upstream_id, r.batch_id, r.trigger, r.model, r.status, r.summary,
	(SELECT count(*) FROM probe_results x WHERE x.run_id = r.id),
	(SELECT count(*) FILTER (WHERE x.passed) FROM probe_results x WHERE x.run_id = r.id),
	r.queued_at, r.finished_at`

func scanRunSummary(row pgx.CollectableRow) (RunSummary, error) {
	var s RunSummary
	var summary []byte
	err := row.Scan(&s.ID, &s.UpstreamID, &s.BatchID, &s.Trigger, &s.Model, &s.Status, &summary, &s.Items, &s.Passed, &s.QueuedAt, &s.FinishedAt)
	if len(summary) > 0 {
		s.Summary = summary
	}
	return s, err
}

// UpstreamRuns 返回某个上游最近的检测记录（不含基线对照检测），供管理员查看明细。
func UpstreamRuns(ctx context.Context, db DB, upstreamID int64, limit int) ([]RunSummary, error) {
	rows, err := db.Query(ctx, "SELECT "+runSummaryColumns+` FROM probe_runs r
		WHERE r.upstream_id = $1 AND r.trigger <> 'reference' ORDER BY r.id DESC LIMIT $2`, upstreamID, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanRunSummary)
}

// GetRunDetail 返回一次检测的逐题结果；被测上游的检测附带同批次基线账号的结果。
func GetRunDetail(ctx context.Context, db DB, runID int64) (*RunDetail, error) {
	rows, err := db.Query(ctx, "SELECT "+runSummaryColumns+" FROM probe_runs r WHERE r.id = $1", runID)
	if err != nil {
		return nil, err
	}
	runs, err := pgx.CollectRows(rows, scanRunSummary)
	if err != nil {
		return nil, err
	}
	if len(runs) == 0 {
		return nil, httpx.NotFound("RUN_NOT_FOUND", "probe run not found")
	}
	out := &RunDetail{Run: runs[0]}
	if out.Results, err = runResults(ctx, db, runID); err != nil {
		return nil, err
	}
	if out.Run.Trigger == "reference" {
		return out, nil
	}
	rows, err = db.Query(ctx, "SELECT "+runSummaryColumns+` FROM probe_runs r
		WHERE r.batch_id = $1 AND r.trigger = 'reference' ORDER BY r.id LIMIT 1`, out.Run.BatchID)
	if err != nil {
		return nil, err
	}
	refs, err := pgx.CollectRows(rows, scanRunSummary)
	if err != nil {
		return nil, err
	}
	if len(refs) > 0 {
		out.Reference = &refs[0]
		if out.RefResults, err = runResults(ctx, db, refs[0].ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func runResults(ctx context.Context, db DB, runID int64) ([]RunResult, error) {
	rows, err := db.Query(ctx, `SELECT pr.item_id, pr.suite, COALESCE(i.category, ''), COALESCE(i.prompt, ''), COALESCE(i.models, '{}'),
			pr.passed, pr.http_status, pr.error_class, COALESCE(pr.ttft_ms, 0), COALESCE(pr.duration_ms, 0),
			COALESCE(pr.output_tokens, 0), COALESCE(pr.reasoning_tokens, 0), pr.response_model, pr.raw_excerpt, pr.check_key, pr.instance_idx, pr.step, pr.instance_hash, pr.generator_version, pr.input_tokens, pr.cache_read_tokens, pr.cache_creation_tokens, pr.stop_reason, pr.effort, pr.flags, pr.error_body
		FROM probe_results pr LEFT JOIN probe_items i ON i.id = pr.item_id
		WHERE pr.run_id = $1 ORDER BY pr.id`, runID)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (RunResult, error) {
		var r RunResult
		err := row.Scan(&r.ItemID, &r.Suite, &r.Category, &r.Prompt, &r.Models, &r.Passed, &r.HTTPStatus, &r.ErrorClass,
			&r.TTFTMs, &r.DurationMs, &r.OutputTokens, &r.ReasoningTokens, &r.ResponseModel, &r.Excerpt, &r.CheckKey, &r.InstanceIdx, &r.Step, &r.InstanceHash, &r.GeneratorVersion, &r.InputTokens, &r.CacheReadTokens, &r.CacheCreationTokens, &r.StopReason, &r.Effort, &r.Flags, &r.ErrorBody)
		return r, err
	})
	if out == nil {
		out = []RunResult{}
	}
	return out, err
}
