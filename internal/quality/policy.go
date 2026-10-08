// Package quality 负责上游质量检测：厂商检测策略、题库、主动探测、基线对照与序贯检验判定降智、
// 归因、处置与恢复、卖家测试报告与申诉、被动信号。
package quality

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/DueGin/token-market/internal/listing"
	"github.com/DueGin/token-market/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type DB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Thresholds struct {
	H0    float64 `json:"h0"`    // 正常：相对分 ≥ h0
	H1    float64 `json:"h1"`    // 降智：相对分 ≤ h1
	Alpha float64 `json:"alpha"` // 误判降智的概率上限
	Beta  float64 `json:"beta"`  // 漏判降智的概率上限
}

func (t Thresholds) validate() error {
	if t.H1 <= 0 || t.H0 <= t.H1 || t.H0 > 1 || t.Alpha <= 0 || t.Alpha >= 0.5 || t.Beta <= 0 || t.Beta >= 0.5 {
		return httpx.BadRequest("INVALID_THRESHOLDS", "need 0 < h1 < h0 <= 1 and 0 < alpha, beta < 0.5")
	}
	return nil
}

// Policy 是一个厂商（可细化到接入方式）的检测策略。
type Policy struct {
	ID               int64      `json:"id"`
	Platform         string     `json:"platform"`
	AuthType         string     `json:"auth_type"`
	Enabled          bool       `json:"enabled"`
	ScheduledEnabled bool       `json:"scheduled_enabled"`
	IntervalMinutes  int        `json:"interval_minutes"`
	JitterMinutes    int        `json:"jitter_minutes"`
	Suites           []string   `json:"suites"`
	Models           []string   `json:"models"`
	DailyBudgetUSD   float64    `json:"daily_budget_usd"`
	ItemsPerRun      int        `json:"items_per_run"`
	Thresholds       Thresholds `json:"thresholds"`
	AutoPause        bool       `json:"auto_pause"`
	DefaultGrade     string     `json:"default_grade"`
	Version          int64      `json:"version"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

const policyColumns = `id, platform, auth_type, enabled, scheduled_enabled, interval_minutes, jitter_minutes, suites, models,
	daily_budget_usd::float8, items_per_run, thresholds, auto_pause, default_grade, version, updated_at`

func scanPolicy(row pgx.Row) (*Policy, error) {
	var p Policy
	var th []byte
	if err := row.Scan(&p.ID, &p.Platform, &p.AuthType, &p.Enabled, &p.ScheduledEnabled, &p.IntervalMinutes, &p.JitterMinutes,
		&p.Suites, &p.Models, &p.DailyBudgetUSD, &p.ItemsPerRun, &th, &p.AutoPause, &p.DefaultGrade,
		&p.Version, &p.UpdatedAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(th, &p.Thresholds); err != nil {
		return nil, err
	}
	return &p, nil
}

func ListPolicies(ctx context.Context, db DB) ([]Policy, error) {
	rows, err := db.Query(ctx, "SELECT "+policyColumns+" FROM detection_policies ORDER BY platform, auth_type")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Policy
	for rows.Next() {
		p, err := scanPolicy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// EffectivePolicy 返回上游适用的策略：先找精确匹配接入方式的，再找厂商通用的，最后叠加单上游覆盖。
func EffectivePolicy(ctx context.Context, db DB, upstreamID int64, platform, authType string) (*Policy, error) {
	p, err := scanPolicy(db.QueryRow(ctx, "SELECT "+policyColumns+` FROM detection_policies
		WHERE platform = $1 AND auth_type IN ($2, '') ORDER BY (auth_type = $2) DESC LIMIT 1`, platform, authType))
	if errors.Is(err, pgx.ErrNoRows) {
		// 没有配置策略的厂商视为不检测
		return &Policy{Platform: platform, DefaultGrade: "B", Thresholds: Thresholds{H0: 0.9, H1: 0.7, Alpha: 0.05, Beta: 0.1}}, nil
	}
	if err != nil {
		return nil, err
	}
	var enabled, scheduled *bool
	var expires *time.Time
	err = db.QueryRow(ctx, `SELECT enabled, scheduled_enabled, expires_at FROM detection_overrides WHERE upstream_id = $1`, upstreamID).
		Scan(&enabled, &scheduled, &expires)
	if err == nil && (expires == nil || time.Now().Before(*expires)) {
		if enabled != nil {
			p.Enabled = *enabled
		}
		if scheduled != nil {
			p.ScheduledEnabled = *scheduled
		}
	} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if !p.Enabled {
		p.ScheduledEnabled = false
	}
	return p, nil
}

var validSuites = map[string]bool{"availability": true, "latency": true, "iq": true, "reasoning": true, "long_context": true}

type PolicyInput struct {
	Enabled          *bool       `json:"enabled"`
	ScheduledEnabled *bool       `json:"scheduled_enabled"`
	IntervalMinutes  *int        `json:"interval_minutes"`
	JitterMinutes    *int        `json:"jitter_minutes"`
	Suites           *[]string   `json:"suites"`
	Models           *[]string   `json:"models"`
	DailyBudgetUSD   *float64    `json:"daily_budget_usd"`
	ItemsPerRun      *int        `json:"items_per_run"`
	Thresholds       *Thresholds `json:"thresholds"`
	AutoPause        *bool       `json:"auto_pause"`
	DefaultGrade     *string     `json:"default_grade"`
}

// UpsertPolicy 创建或修改策略，修改后版本号加一，worker 在下一轮读取新策略（约 1 分钟内生效）。
func UpsertPolicy(ctx context.Context, db DB, platform, authType string, in PolicyInput, adminID int64) (*Policy, error) {
	if _, err := listing.GetVendor(ctx, db, platform); err != nil {
		return nil, err
	}
	p, err := scanPolicy(db.QueryRow(ctx, "SELECT "+policyColumns+" FROM detection_policies WHERE platform = $1 AND auth_type = $2", platform, authType))
	if errors.Is(err, pgx.ErrNoRows) {
		// 新建接入方式级策略时以厂商通用策略为模板
		p, err = scanPolicy(db.QueryRow(ctx, "SELECT "+policyColumns+" FROM detection_policies WHERE platform = $1 AND auth_type = ''", platform))
		if errors.Is(err, pgx.ErrNoRows) {
			p = &Policy{Platform: platform, IntervalMinutes: 360, JitterMinutes: 30, Suites: []string{"availability", "latency", "iq"},
				DailyBudgetUSD: 1, ItemsPerRun: 12, Thresholds: Thresholds{H0: 0.9, H1: 0.7, Alpha: 0.05, Beta: 0.1}, AutoPause: true, DefaultGrade: "B"}
		} else if err != nil {
			return nil, err
		}
		p.AuthType = authType
	} else if err != nil {
		return nil, err
	}
	if p.Models == nil {
		p.Models = []string{}
	}
	if in.Enabled != nil {
		p.Enabled = *in.Enabled
	}
	if in.ScheduledEnabled != nil {
		p.ScheduledEnabled = *in.ScheduledEnabled
	}
	if in.IntervalMinutes != nil {
		p.IntervalMinutes = *in.IntervalMinutes
	}
	if in.JitterMinutes != nil {
		p.JitterMinutes = *in.JitterMinutes
	}
	if in.Suites != nil {
		p.Suites = *in.Suites
	}
	if in.Models != nil {
		p.Models = *in.Models
	}
	if in.DailyBudgetUSD != nil {
		p.DailyBudgetUSD = *in.DailyBudgetUSD
	}
	if in.ItemsPerRun != nil {
		p.ItemsPerRun = *in.ItemsPerRun
	}
	if in.Thresholds != nil {
		p.Thresholds = *in.Thresholds
	}
	if in.AutoPause != nil {
		p.AutoPause = *in.AutoPause
	}
	if in.DefaultGrade != nil {
		p.DefaultGrade = *in.DefaultGrade
	}
	switch {
	case p.ScheduledEnabled && !p.Enabled:
		return nil, httpx.BadRequest("INVALID_POLICY", "scheduled detection requires detection to be enabled")
	case p.IntervalMinutes < 10 || p.JitterMinutes < 0 || p.JitterMinutes >= p.IntervalMinutes:
		return nil, httpx.BadRequest("INVALID_INTERVAL", "interval must be >= 10 minutes and jitter smaller than interval")
	case p.ItemsPerRun <= 0 || p.ItemsPerRun > 200:
		return nil, httpx.BadRequest("INVALID_ITEMS", "items_per_run must be between 1 and 200")
	case p.DailyBudgetUSD < 0:
		return nil, httpx.BadRequest("INVALID_BUDGET", "daily_budget_usd must be >= 0")
	case p.Enabled && len(p.Models) == 0:
		return nil, httpx.BadRequest("MODELS_REQUIRED", "at least one model is required when detection is enabled")
	}
	switch p.DefaultGrade {
	case "S", "A", "B", "C", "D":
	default:
		return nil, httpx.BadRequest("INVALID_GRADE", "default_grade must be S, A, B, C or D")
	}
	for _, s := range p.Suites {
		if !validSuites[s] {
			return nil, httpx.BadRequest("INVALID_SUITE", "unknown suite "+s)
		}
	}
	if err := p.Thresholds.validate(); err != nil {
		return nil, err
	}
	th, _ := json.Marshal(p.Thresholds)
	return scanPolicy(db.QueryRow(ctx, `INSERT INTO detection_policies (platform, auth_type, enabled, scheduled_enabled, interval_minutes,
			jitter_minutes, suites, models, daily_budget_usd, items_per_run, thresholds, auto_pause, default_grade, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		ON CONFLICT (platform, auth_type) DO UPDATE SET enabled = EXCLUDED.enabled, scheduled_enabled = EXCLUDED.scheduled_enabled,
			interval_minutes = EXCLUDED.interval_minutes, jitter_minutes = EXCLUDED.jitter_minutes, suites = EXCLUDED.suites,
			models = EXCLUDED.models, daily_budget_usd = EXCLUDED.daily_budget_usd, items_per_run = EXCLUDED.items_per_run,
			thresholds = EXCLUDED.thresholds, auto_pause = EXCLUDED.auto_pause,
			default_grade = EXCLUDED.default_grade, updated_by = EXCLUDED.updated_by,
			version = detection_policies.version + 1, updated_at = now()
		RETURNING `+policyColumns,
		platform, authType, p.Enabled, p.ScheduledEnabled, p.IntervalMinutes, p.JitterMinutes, p.Suites, p.Models, p.DailyBudgetUSD,
		p.ItemsPerRun, th, p.AutoPause, p.DefaultGrade, adminID))
}

type OverrideInput struct {
	Enabled          *bool      `json:"enabled"`
	ScheduledEnabled *bool      `json:"scheduled_enabled"`
	ExpiresAt        *time.Time `json:"expires_at"`
	Reason           string     `json:"reason"`
}

// SetOverride 管理员对单个上游强制开启或关闭检测。
func SetOverride(ctx context.Context, db DB, upstreamID int64, in OverrideInput, adminID int64) error {
	_, err := db.Exec(ctx, `INSERT INTO detection_overrides (upstream_id, enabled, scheduled_enabled, expires_at, reason, updated_by)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6)
		ON CONFLICT (upstream_id) DO UPDATE SET enabled = EXCLUDED.enabled, scheduled_enabled = EXCLUDED.scheduled_enabled,
			expires_at = EXCLUDED.expires_at, reason = EXCLUDED.reason, updated_by = EXCLUDED.updated_by, updated_at = now()`,
		upstreamID, in.Enabled, in.ScheduledEnabled, in.ExpiresAt, in.Reason, adminID)
	return err
}

func DeleteOverride(ctx context.Context, db DB, upstreamID int64) error {
	_, err := db.Exec(ctx, "DELETE FROM detection_overrides WHERE upstream_id = $1", upstreamID)
	return err
}

// ---- 题库 ----

type Grader struct {
	Type            string   `json:"type"` // exact / contains / regex / numeric / choice / json_keys
	Answer          string   `json:"answer,omitempty"`
	Number          *float64 `json:"number,omitempty"`
	Tolerance       float64  `json:"tolerance,omitempty"`
	Pattern         string   `json:"pattern,omitempty"`
	Keys            []string `json:"keys,omitempty"`
	CaseInsensitive bool     `json:"case_insensitive,omitempty"`
}

type Item struct {
	ID                int64    `json:"id"`
	Suite             string   `json:"suite"`
	Category          string   `json:"category"`
	Prompt            string   `json:"prompt"`
	Grader            Grader   `json:"grader"`
	Platforms         []string `json:"platforms"`
	MaxTokens         int      `json:"max_tokens"`
	ReasoningEffort   *string  `json:"reasoning_effort,omitempty"`
	IRTDifficulty     *float64 `json:"irt_difficulty,omitempty"`
	IRTDiscrimination *float64 `json:"irt_discrimination,omitempty"`
	Status            string   `json:"status"`
	Version           int      `json:"version"`
}

const itemColumns = `id, suite, category, prompt, grader, platforms, max_tokens, reasoning_effort, irt_difficulty, irt_discrimination, status, version`

func scanItem(row pgx.Row) (*Item, error) {
	var it Item
	var g []byte
	if err := row.Scan(&it.ID, &it.Suite, &it.Category, &it.Prompt, &g, &it.Platforms, &it.MaxTokens, &it.ReasoningEffort,
		&it.IRTDifficulty, &it.IRTDiscrimination, &it.Status, &it.Version); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(g, &it.Grader); err != nil {
		return nil, err
	}
	return &it, nil
}

func validateItem(it Item) error {
	switch it.Suite {
	case "iq", "reasoning", "long_context":
	default:
		return httpx.BadRequest("INVALID_SUITE", "item suite must be iq, reasoning or long_context")
	}
	if it.Prompt == "" || it.MaxTokens <= 0 {
		return httpx.BadRequest("INVALID_ITEM", "prompt and max_tokens are required")
	}
	if _, err := grade(it.Grader, "", ""); err != nil {
		return httpx.BadRequest("INVALID_GRADER", err.Error())
	}
	return nil
}

func CreateItem(ctx context.Context, db DB, it Item) (*Item, error) {
	if it.Status == "" {
		it.Status = "active"
	}
	if it.MaxTokens == 0 {
		it.MaxTokens = 1024
	}
	if err := validateItem(it); err != nil {
		return nil, err
	}
	g, _ := json.Marshal(it.Grader)
	if it.Platforms == nil {
		it.Platforms = []string{}
	}
	return scanItem(db.QueryRow(ctx, `INSERT INTO probe_items (suite, category, prompt, grader, platforms, max_tokens, reasoning_effort,
			irt_difficulty, irt_discrimination, status) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING `+itemColumns,
		it.Suite, it.Category, it.Prompt, g, it.Platforms, it.MaxTokens, it.ReasoningEffort, it.IRTDifficulty, it.IRTDiscrimination, it.Status))
}

func UpdateItem(ctx context.Context, db DB, id int64, it Item) (*Item, error) {
	if err := validateItem(it); err != nil {
		return nil, err
	}
	g, _ := json.Marshal(it.Grader)
	if it.Platforms == nil {
		it.Platforms = []string{}
	}
	out, err := scanItem(db.QueryRow(ctx, `UPDATE probe_items SET suite = $2, category = $3, prompt = $4, grader = $5, platforms = $6,
			max_tokens = $7, reasoning_effort = $8, irt_difficulty = $9, irt_discrimination = $10, status = $11,
			version = version + 1, updated_at = now() WHERE id = $1 RETURNING `+itemColumns,
		id, it.Suite, it.Category, it.Prompt, g, it.Platforms, it.MaxTokens, it.ReasoningEffort, it.IRTDifficulty, it.IRTDiscrimination, it.Status))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.NotFound("ITEM_NOT_FOUND", "probe item not found")
	}
	return out, err
}

func ListItems(ctx context.Context, db DB, status string) ([]Item, error) {
	rows, err := db.Query(ctx, "SELECT "+itemColumns+" FROM probe_items WHERE $1 = '' OR status = $1 ORDER BY id", status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

// selectItems 按套件为某平台抽题：优先区分度高的题（IRT），其余随机，保证每个启用的套件都有题。
func selectItems(ctx context.Context, db DB, platform string, suites []string, n int, seed int64) ([]Item, error) {
	var wanted []string
	for _, s := range suites {
		if s == "iq" || s == "reasoning" || s == "long_context" {
			wanted = append(wanted, s)
		}
	}
	if len(wanted) == 0 {
		// 只启用可用性 / 延迟套件时用 iq 题作为请求载体，但不计入智力评分
		wanted = []string{"iq"}
	}
	rows, err := db.Query(ctx, "SELECT "+itemColumns+` FROM probe_items
		WHERE status = 'active' AND suite = ANY($1) AND (cardinality(platforms) = 0 OR $2 = ANY(platforms))
		ORDER BY COALESCE(irt_discrimination, 0) DESC, md5(id::text || $3::text)`, wanted, platform, fmt.Sprint(seed))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var all []Item
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		all = append(all, *it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// 轮流从每个套件取题
	bySuite := map[string][]Item{}
	for _, it := range all {
		bySuite[it.Suite] = append(bySuite[it.Suite], it)
	}
	var out []Item
	for len(out) < n {
		progressed := false
		for _, s := range wanted {
			if len(bySuite[s]) > 0 && len(out) < n {
				out = append(out, bySuite[s][0])
				bySuite[s] = bySuite[s][1:]
				progressed = true
			}
		}
		if !progressed {
			break
		}
	}
	return out, nil
}
