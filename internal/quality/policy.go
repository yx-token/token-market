// Package quality 负责上游质量检测：厂商检测策略、题库、主动探测、基线对照与序贯检验判定降智、
// 归因、处置与恢复、卖家测试报告与申诉、被动信号。
package quality

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/DueGin/token-market/internal/inspection"
	"github.com/DueGin/token-market/internal/listing"
	"github.com/DueGin/token-market/internal/platform/httpx"
	"github.com/DueGin/token-market/internal/quality/thirdparty"
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
	ItemParallelism  int                    `json:"item_parallelism"`
	FullTriggers     []string               `json:"full_triggers"`
	TokenBudgets     map[string]TokenBudget `json:"token_budgets"`
	ID               int64                  `json:"id"`
	Platform         string                 `json:"platform"`
	AuthType         string                 `json:"auth_type"`
	Enabled          bool                   `json:"enabled"`
	ScheduledEnabled bool                   `json:"scheduled_enabled"`
	IntervalMinutes  int                    `json:"interval_minutes"`
	JitterMinutes    int                    `json:"jitter_minutes"`
	Suites           []string               `json:"suites"`
	Models           []string               `json:"models"`
	DailyBudgetUSD   float64                `json:"daily_budget_usd"`
	Thresholds       Thresholds             `json:"thresholds"`
	AutoPause        bool                   `json:"auto_pause"`
	DefaultGrade     string                 `json:"default_grade"`
	// ThirdParty 是在内部探测之后追加的第三方检测
	ThirdParty []ThirdPartyCheck `json:"third_party"`
	Version    int64             `json:"version"`
	UpdatedAt  time.Time         `json:"updated_at"`
}

// ThirdPartyCheck 是策略中的一项第三方检测：在哪些检测类型中执行，以及检测站的结论如何折算成平台的判定。
type ThirdPartyCheck struct {
	Provider string   `json:"provider"`
	Triggers []string `json:"triggers"`         // onboarding / scheduled / recovery / seller_manual / admin / adaptive / appeal
	Model    string   `json:"model,omitempty"`  // 为空时使用本次内部探测的模型
	Format   string   `json:"format,omitempty"` // openai / anthropic，为空时按平台推断
	// 身份不一致（其他家族或伪装）与仅具体型号不一致时的处置：degraded / suspected / ignore
	OnMismatch         string `json:"on_mismatch"`
	OnSubmodelMismatch string `json:"on_submodel_mismatch"`
}

// Applies 判断该检测项是否在某种检测类型中执行。
func (c ThirdPartyCheck) Applies(trigger string) bool {
	for _, t := range c.Triggers {
		if t == trigger {
			return true
		}
	}
	return false
}

// DefaultFormat 按平台推断第三方检测使用的协议：Claude 用 Messages，其余用 Chat Completions（sub2api 网关都支持）。
func DefaultFormat(platform string) string {
	if platform == "anthropic" {
		return "anthropic"
	}
	return "openai"
}

var validTriggers = map[string]bool{"onboarding": true, "scheduled": true, "recovery": true, "seller_manual": true, "admin": true,
	"adaptive": true, "appeal": true}

func (c *ThirdPartyCheck) normalize() error {
	if _, ok := thirdparty.Lookup(c.Provider); !ok {
		return httpx.BadRequest("UNKNOWN_PROVIDER", "unknown detection provider "+c.Provider)
	}
	if len(c.Triggers) == 0 {
		return httpx.BadRequest("INVALID_THIRD_PARTY", "third-party check needs at least one trigger")
	}
	for _, t := range c.Triggers {
		if !validTriggers[t] {
			return httpx.BadRequest("INVALID_THIRD_PARTY", "unknown trigger "+t)
		}
	}
	if c.Format != "" && c.Format != "openai" && c.Format != "anthropic" {
		return httpx.BadRequest("INVALID_THIRD_PARTY", "format must be openai or anthropic")
	}
	if c.OnMismatch == "" {
		c.OnMismatch = "degraded"
	}
	if c.OnSubmodelMismatch == "" {
		c.OnSubmodelMismatch = "suspected"
	}
	for _, a := range []string{c.OnMismatch, c.OnSubmodelMismatch} {
		if a != "degraded" && a != "suspected" && a != "ignore" {
			return httpx.BadRequest("INVALID_THIRD_PARTY", "action must be degraded, suspected or ignore")
		}
	}
	return nil
}

// VerdictFor 把检测站的归类折算成平台的判定。
func (c ThirdPartyCheck) VerdictFor(outcome string) string {
	action := ""
	switch outcome {
	case thirdparty.OutcomePass:
		return "normal"
	case thirdparty.OutcomeMismatch, thirdparty.OutcomeSpoof:
		action = c.OnMismatch
	case thirdparty.OutcomeSubmodelMismatch:
		action = c.OnSubmodelMismatch
	}
	if action == "degraded" || action == "suspected" {
		return action
	}
	return "inconclusive"
}

const policyColumns = `id, platform, auth_type, enabled, scheduled_enabled, interval_minutes, jitter_minutes, suites, models,
	daily_budget_usd::float8, thresholds, auto_pause, default_grade, third_party, version, updated_at, item_parallelism, full_triggers, token_budgets`

func scanPolicy(row pgx.Row) (*Policy, error) {
	var p Policy
	var th, tp, budgets []byte
	if err := row.Scan(&p.ID, &p.Platform, &p.AuthType, &p.Enabled, &p.ScheduledEnabled, &p.IntervalMinutes, &p.JitterMinutes,
		&p.Suites, &p.Models, &p.DailyBudgetUSD, &th, &p.AutoPause, &p.DefaultGrade, &tp,
		&p.Version, &p.UpdatedAt, &p.ItemParallelism, &p.FullTriggers, &budgets); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(th, &p.Thresholds); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(tp, &p.ThirdParty); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(budgets, &p.TokenBudgets); err != nil {
		return nil, err
	}
	if p.ThirdParty == nil {
		p.ThirdParty = []ThirdPartyCheck{}
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
		return &Policy{Platform: platform, DefaultGrade: "B", Thresholds: Thresholds{H0: 0.9, H1: 0.7, Alpha: 0.05, Beta: 0.1},
			ThirdParty: []ThirdPartyCheck{}}, nil
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

var validSuites = map[string]bool{"availability": true, "latency": true, "iq": true, "reasoning": true, "long_context": true, "inspection_authenticity": true, "inspection_capability": true, "inspection_context": true, "inspection_function": true, "inspection_output": true, "inspection_performance": true}

type PolicyInput struct {
	ItemParallelism  *int                    `json:"item_parallelism"`
	FullTriggers     *[]string               `json:"full_triggers"`
	TokenBudgets     *map[string]TokenBudget `json:"token_budgets"`
	Enabled          *bool                   `json:"enabled"`
	ScheduledEnabled *bool                   `json:"scheduled_enabled"`
	IntervalMinutes  *int                    `json:"interval_minutes"`
	JitterMinutes    *int                    `json:"jitter_minutes"`
	Suites           *[]string               `json:"suites"`
	Models           *[]string               `json:"models"`
	DailyBudgetUSD   *float64                `json:"daily_budget_usd"`
	Thresholds       *Thresholds             `json:"thresholds"`
	AutoPause        *bool                   `json:"auto_pause"`
	DefaultGrade     *string                 `json:"default_grade"`
	ThirdParty       *[]ThirdPartyCheck      `json:"third_party"`
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
				DailyBudgetUSD: 1, Thresholds: Thresholds{H0: 0.9, H1: 0.7, Alpha: 0.05, Beta: 0.1}, AutoPause: true, DefaultGrade: "B"}
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
	if in.Thresholds != nil {
		p.Thresholds = *in.Thresholds
	}
	if in.AutoPause != nil {
		p.AutoPause = *in.AutoPause
	}
	if in.DefaultGrade != nil {
		p.DefaultGrade = *in.DefaultGrade
	}
	if in.ThirdParty != nil {
		p.ThirdParty = *in.ThirdParty
	}

	if p.ItemParallelism == 0 {
		p.ItemParallelism = 4
	}
	if p.FullTriggers == nil {
		p.FullTriggers = []string{"onboarding", "appeal", "admin"}
	}
	if p.TokenBudgets == nil {
		p.TokenBudgets = map[string]TokenBudget{}
	}
	if in.ItemParallelism != nil {
		p.ItemParallelism = *in.ItemParallelism
	}
	if in.FullTriggers != nil {
		p.FullTriggers = *in.FullTriggers
	}
	if in.TokenBudgets != nil {
		p.TokenBudgets = *in.TokenBudgets
	}
	if p.ItemParallelism < 1 || p.ItemParallelism > 8 {
		return nil, httpx.BadRequest("INVALID_PARALLELISM", "题目并发上限须为 1–8")
	}
	for _, t := range p.FullTriggers {
		if !validTriggers[t] {
			return nil, httpx.BadRequest("INVALID_TRIGGER", "未知完整质检触发类型")
		}
	}
	for mode, b := range p.TokenBudgets {
		if (mode != "full" && mode != "quick") || b.Input <= 0 || b.Output <= 0 {
			return nil, httpx.BadRequest("INVALID_BUDGET", "质检模式及 token 预算无效")
		}
	}
	if p.ThirdParty == nil {
		p.ThirdParty = []ThirdPartyCheck{}
	}
	seen := map[string]bool{}
	for i := range p.ThirdParty {
		if err := p.ThirdParty[i].normalize(); err != nil {
			return nil, err
		}
		if seen[p.ThirdParty[i].Provider] {
			return nil, httpx.BadRequest("INVALID_THIRD_PARTY", "each provider can appear only once")
		}
		seen[p.ThirdParty[i].Provider] = true
	}
	switch {
	case p.ScheduledEnabled && !p.Enabled:
		return nil, httpx.BadRequest("INVALID_POLICY", "scheduled detection requires detection to be enabled")
	case p.IntervalMinutes < 10 || p.JitterMinutes < 0 || p.JitterMinutes >= p.IntervalMinutes:
		return nil, httpx.BadRequest("INVALID_INTERVAL", "interval must be >= 10 minutes and jitter smaller than interval")
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
	tp, _ := json.Marshal(p.ThirdParty)
	return scanPolicy(db.QueryRow(ctx, `INSERT INTO detection_policies (platform, auth_type, enabled, scheduled_enabled, interval_minutes,
			jitter_minutes, suites, models, daily_budget_usd, thresholds, auto_pause, default_grade, third_party, updated_by, item_parallelism, full_triggers, token_budgets)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $14, $13, $15, $16, $17)
		ON CONFLICT (platform, auth_type) DO UPDATE SET enabled = EXCLUDED.enabled, scheduled_enabled = EXCLUDED.scheduled_enabled,
			interval_minutes = EXCLUDED.interval_minutes, jitter_minutes = EXCLUDED.jitter_minutes, suites = EXCLUDED.suites,
			models = EXCLUDED.models, daily_budget_usd = EXCLUDED.daily_budget_usd,
			thresholds = EXCLUDED.thresholds, auto_pause = EXCLUDED.auto_pause,
			default_grade = EXCLUDED.default_grade, third_party = EXCLUDED.third_party, updated_by = EXCLUDED.updated_by,
			item_parallelism = EXCLUDED.item_parallelism, full_triggers = EXCLUDED.full_triggers, token_budgets = EXCLUDED.token_budgets,
			version = detection_policies.version + 1, updated_at = now()
		RETURNING `+policyColumns,
		platform, authType, p.Enabled, p.ScheduledEnabled, p.IntervalMinutes, p.JitterMinutes, p.Suites, p.Models, p.DailyBudgetUSD,
		th, p.AutoPause, p.DefaultGrade, adminID, tp, p.ItemParallelism, p.FullTriggers, mustJSON(p.TokenBudgets)))
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
	GraderOptions
	Type            string   `json:"type"` // exact / contains / regex / numeric / choice / json_keys
	Answer          string   `json:"answer,omitempty"`
	Number          *float64 `json:"number,omitempty"`
	Tolerance       float64  `json:"tolerance,omitempty"`
	Pattern         string   `json:"pattern,omitempty"`
	Keys            []string `json:"keys,omitempty"`
	CaseInsensitive bool     `json:"case_insensitive,omitempty"`
}

type Item struct {
	CheckKey          string          `json:"check_key"`
	Request           ProbeRequest    `json:"request"`
	Generator         json.RawMessage `json:"generator"`
	Modes             []string        `json:"modes"`
	BuiltinKey        *string         `json:"builtin_key,omitempty"`
	Weight            float64         `json:"weight"`
	ID                int64           `json:"id"`
	Suite             string          `json:"suite"`
	Category          string          `json:"category"`
	Prompt            string          `json:"prompt"`
	Grader            Grader          `json:"grader"`
	Platforms         []string        `json:"platforms"`
	Models            []string        `json:"models"` // 适用模型，空表示全部
	MaxTokens         int             `json:"max_tokens"`
	ReasoningEffort   *string         `json:"reasoning_effort,omitempty"`
	IRTDifficulty     *float64        `json:"irt_difficulty,omitempty"`
	IRTDiscrimination *float64        `json:"irt_discrimination,omitempty"`
	Status            string          `json:"status"`
	Version           int             `json:"version"`
}

const itemColumns = `id, suite, category, prompt, grader, platforms, models, max_tokens, reasoning_effort, irt_difficulty, irt_discrimination, status, version, check_key, request, generator, modes, builtin_key, weight`

func scanItem(row pgx.Row) (*Item, error) {
	var it Item
	var g, request []byte
	if err := row.Scan(&it.ID, &it.Suite, &it.Category, &it.Prompt, &g, &it.Platforms, &it.Models, &it.MaxTokens, &it.ReasoningEffort,
		&it.IRTDifficulty, &it.IRTDiscrimination, &it.Status, &it.Version, &it.CheckKey, &request, &it.Generator, &it.Modes, &it.BuiltinKey, &it.Weight); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(g, &it.Grader); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(request, &it.Request); err != nil {
		return nil, err
	}
	return &it, nil
}

func validateItem(it Item) error {
	switch it.Suite {
	case "iq", "reasoning", "long_context", "inspection_authenticity", "inspection_capability", "inspection_context", "inspection_function", "inspection_output", "inspection_performance":
	default:
		return httpx.BadRequest("INVALID_SUITE", "item suite must be iq, reasoning or long_context")
	}
	// 质检项键须为注册表已知项；拼错的键不会被任何判定读取，题目会被静默忽略。
	if it.CheckKey != "" {
		if _, ok := inspection.Lookup(it.CheckKey); !ok {
			return httpx.BadRequest("INVALID_CHECK_KEY", "未知质检项："+it.CheckKey)
		}
	}
	d, err := parseGenerator(it.Generator)
	if err != nil {
		return httpx.BadRequest("INVALID_GENERATOR", err.Error())
	}
	if (it.Prompt == "" && it.Request.Prompt == "" && len(it.Request.Messages) == 0 && d == nil) || it.MaxTokens <= 0 {
		return httpx.BadRequest("INVALID_ITEM", "题面或请求内容与正数 max_tokens 必填")
	}
	if d != nil {
		if !strings.HasPrefix(it.Suite, "inspection_") {
			return httpx.BadRequest("INVALID_SUITE", "生成题须使用新质检套件")
		}
		if _, err := generateBankProbe(*d, strings.Repeat("1", 64)); err != nil {
			return httpx.BadRequest("INVALID_GENERATOR", err.Error())
		}
	}
	for _, mode := range it.Modes {
		if mode != "full" && mode != "quick" {
			return httpx.BadRequest("INVALID_MODES", "题目模式须为 full 或 quick")
		}
	}
	var gradeErr error
	if inspectionGraderTypes[it.Grader.Type] {
		gradeErr = validateInspectionGrader(it.Grader)
	} else {
		_, gradeErr = grade(it.Grader, "", "")
	}
	if gradeErr != nil {
		return httpx.BadRequest("INVALID_GRADER", gradeErr.Error())
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
	if it.Modes == nil {
		it.Modes = []string{}
	}
	if len(it.Generator) == 0 {
		it.Generator = json.RawMessage(`{}`)
	}
	if it.Weight == 0 {
		it.Weight = 1
	}
	g, _ := json.Marshal(it.Grader)
	if it.Platforms == nil {
		it.Platforms = []string{}
	}
	if it.Models == nil {
		it.Models = []string{}
	}
	return scanItem(db.QueryRow(ctx, `INSERT INTO probe_items (suite, category, prompt, grader, platforms, models, max_tokens, reasoning_effort,
			irt_difficulty, irt_discrimination, status, check_key, request, generator, modes, builtin_key, weight) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17) RETURNING `+itemColumns,
		it.Suite, it.Category, it.Prompt, g, it.Platforms, it.Models, it.MaxTokens, it.ReasoningEffort, it.IRTDifficulty, it.IRTDiscrimination, it.Status, it.CheckKey, mustJSON(it.Request), it.Generator, it.Modes, it.BuiltinKey, it.Weight))
}

func UpdateItem(ctx context.Context, db DB, id int64, it Item) (*Item, error) {
	if err := validateItem(it); err != nil {
		return nil, err
	}
	if it.Modes == nil {
		it.Modes = []string{}
	}
	if len(it.Generator) == 0 {
		it.Generator = json.RawMessage(`{}`)
	}
	if it.Weight == 0 {
		it.Weight = 1
	}
	g, _ := json.Marshal(it.Grader)
	if it.Platforms == nil {
		it.Platforms = []string{}
	}
	if it.Models == nil {
		it.Models = []string{}
	}
	out, err := scanItem(db.QueryRow(ctx, `UPDATE probe_items SET suite = $2, category = $3, prompt = $4, grader = $5, platforms = $6,
			models = $7, max_tokens = $8, reasoning_effort = $9, irt_difficulty = $10, irt_discrimination = $11, status = $12,
			check_key = $13, request = $14, generator = $15, modes = $16, builtin_key = COALESCE($17,builtin_key), weight = $18,
			version = version + 1, updated_at = now() WHERE id = $1 RETURNING `+itemColumns,
		id, it.Suite, it.Category, it.Prompt, g, it.Platforms, it.Models, it.MaxTokens, it.ReasoningEffort, it.IRTDifficulty, it.IRTDiscrimination, it.Status, it.CheckKey, mustJSON(it.Request), it.Generator, it.Modes, it.BuiltinKey, it.Weight))
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

// selectItems 返回一次检测要测的全部题目，不抽样：启用套件下所有使用中的题，
// 且题目的适用平台、适用模型包含本次检测的平台和模型（为空表示不限）。
func selectItems(ctx context.Context, db DB, platform, model string, suites []string) ([]Item, error) {
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
			AND (cardinality(models) = 0 OR $3 = ANY(models))
		ORDER BY id`, wanted, platform, model)
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
