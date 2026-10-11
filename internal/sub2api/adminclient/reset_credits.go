package adminclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// 重置卡相关接口，字段对照 sub2api v0.2.14：
// OpenAI（Codex）见 handler/admin/openai_oauth_handler.go 与 service/openai_quota_service.go，
// Claude 见 handler/admin/claude_reset_handler.go 与 service/claude_reset_credits.go、claude_reset_redeem.go。

// GetAccount 读取单个账号（含 extra）。
func (c *Client) GetAccount(ctx context.Context, id int64) (*Account, error) {
	var out Account
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/admin/accounts/%d", id), nil, "", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RecoverAccountState 清除账号的错误、限流与临时不可调度状态（不改 schedulable 开关）。
func (c *Client) RecoverAccountState(ctx context.Context, id int64) error {
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/admin/accounts/%d/recover-state", id), nil, "", nil)
}

// ---- OpenAI / Codex ----

type OpenAIRateLimitWindow struct {
	UsedPercent        float64 `json:"used_percent"`
	LimitWindowSeconds int64   `json:"limit_window_seconds"`
	ResetAfterSeconds  int64   `json:"reset_after_seconds"`
	ResetAt            int64   `json:"reset_at"`
}

type OpenAIRateLimit struct {
	Allowed         bool                   `json:"allowed"`
	LimitReached    bool                   `json:"limit_reached"`
	PrimaryWindow   *OpenAIRateLimitWindow `json:"primary_window"`
	SecondaryWindow *OpenAIRateLimitWindow `json:"secondary_window"`
}

// OpenAIResetCredits 是 Codex 重置卡：available_count 为可用张数，credits 只带到期时间（不含卡 ID）。
// 同样的结构以 codex_reset_credit_snapshot 键缓存在账号 extra 中。
type OpenAIResetCredits struct {
	AvailableCount int `json:"available_count"`
	Credits        []struct {
		ExpiresAt string `json:"expires_at"`
	} `json:"credits"`
}

type OpenAIQuota struct {
	PlanType              string              `json:"plan_type"`
	RateLimit             *OpenAIRateLimit    `json:"rate_limit"`
	RateLimitResetCredits *OpenAIResetCredits `json:"rate_limit_reset_credits"`
	FetchedAt             int64               `json:"fetched_at"`
}

// RefreshOpenAIQuota 向官方查询 Codex 用量与重置卡，并把重置卡快照写入账号 extra（codex_reset_credit_snapshot）。
func (c *Client) RefreshOpenAIQuota(ctx context.Context, id int64) (*OpenAIQuota, error) {
	var out OpenAIQuota
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/admin/openai/accounts/%d/quota/refresh", id), nil, "", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// OpenAIResetResult 是使用一张 Codex 重置卡的结果。code 透传官方返回（no_credit 表示没有可用的卡）；
// warning_code 非空表示卡已用掉但 sub2api 的后续处理（恢复账号状态、刷新快照）没有全部完成。
type OpenAIResetResult struct {
	Code                  string       `json:"code"`
	WindowsReset          int          `json:"windows_reset"`
	Quota                 *OpenAIQuota `json:"quota"`
	CacheRefreshed        bool         `json:"cache_refreshed"`
	AccountStateRecovered bool         `json:"account_state_recovered"`
	WarningCode           string       `json:"warning_code"`
}

// ResetOpenAIQuota 使用一张 Codex 重置卡。sub2api 这个接口不幂等，每次调用都可能消耗一张卡，
// 调用方必须自行防止重复提交；耗时含一次官方请求与最多 8 秒的后续处理，所以用长超时。
func (c *Client) ResetOpenAIQuota(ctx context.Context, id int64) (*OpenAIResetResult, error) {
	var out OpenAIResetResult
	if err := c.doSlow(ctx, http.MethodPost, fmt.Sprintf("/api/v1/admin/openai/accounts/%d/reset-quota", id), "", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ---- Claude ----

type ClaudeResetCredit struct {
	Label            string             `json:"label"`
	ResetsLeft       int                `json:"resets_left"`
	StartsAt         *time.Time         `json:"starts_at"`
	ExpiresAt        *time.Time         `json:"expires_at"`
	Clears           []string           `json:"clears"` // five_hour / seven_day / seven_day_overage_included
	PercentUsed      map[string]float64 `json:"percent_used"`
	Blocking         []string           `json:"blocking"`
	UseRequiresLimit bool               `json:"use_requires_limit"`
	Redeemable       bool               `json:"redeemable"`
}

// ClaudeResetCredits 是 Claude 账号的重置卡：available_count 只统计当前可以兑换的次数。
type ClaudeResetCredits struct {
	Eligible       bool                `json:"eligible"`
	AvailableCount int                 `json:"available_count"`
	Credits        []ClaudeResetCredit `json:"credits"`
	CooldownUntil  *time.Time          `json:"cooldown_until"`
	WeeklyResetsAt *time.Time          `json:"weekly_resets_at"`
	FetchedAt      time.Time           `json:"fetched_at"`
}

// ClaudeResetCredits 向官方查询 Claude 重置卡（只读，不消耗；sub2api 不缓存）。仅 OAuth 账号可用。
func (c *Client) ClaudeResetCredits(ctx context.Context, id int64) (*ClaudeResetCredits, error) {
	var out ClaudeResetCredits
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/admin/accounts/%d/claude/reset-credits", id), nil, "", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ClaudeRedeemResult 是兑换 Claude 重置卡的结果。outcome：reset（已重置）、already_used、not_limited、
// cooldown、ineligible、unknown（结果不确定，sub2api 会封锁该组织 24 小时以防重复兑换）。
type ClaudeRedeemResult struct {
	Outcome       string              `json:"outcome"`
	Reason        string              `json:"reason"`
	Cleared       []string            `json:"cleared"`
	CooldownUntil *time.Time          `json:"cooldown_until"`
	Credits       *ClaudeResetCredits `json:"credits"`
	Replayed      bool                `json:"replayed"`
}

// RedeemClaudeResetCredit 兑换一次 Claude 重置卡。sub2api 要求幂等键：同一个键重试返回首次结果，不会重复兑换。
func (c *Client) RedeemClaudeResetCredit(ctx context.Context, id int64, idempotencyKey string) (*ClaudeRedeemResult, error) {
	if idempotencyKey == "" {
		return nil, ErrIdempotencyKeyRequired
	}
	var out ClaudeRedeemResult
	if err := c.doSlow(ctx, http.MethodPost, fmt.Sprintf("/api/v1/admin/accounts/%d/claude/reset-credits/redeem", id), idempotencyKey, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// doSlow 用长超时调用耗时较长的写接口，与 do 一样受写入门控约束。
func (c *Client) doSlow(ctx context.Context, method, path, idempotencyKey string, out any) error {
	if c.writeGate != nil {
		if err := c.writeGate(ctx); err != nil {
			return err
		}
	}
	h := http.Header{"X-Api-Key": []string{c.apiKey}}
	if idempotencyKey != "" {
		h.Set("Idempotency-Key", idempotencyKey)
	}
	return c.diagnosticsWire.Do(ctx, method, path, h, nil, out)
}

// ParseOpenAIResetCredits 解析账号 extra 中缓存的 Codex 重置卡快照，没有快照时返回 nil。
func ParseOpenAIResetCredits(extra map[string]any) *OpenAIResetCredits {
	raw, ok := extra["codex_reset_credit_snapshot"]
	if !ok || raw == nil {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var out OpenAIResetCredits
	if err := json.Unmarshal(b, &out); err != nil {
		return nil
	}
	return &out
}

// OpenAIAutoResetState 是 sub2api 自动用卡的运行态（账号 extra 的 codex_auto_reset_credit_state）。
type OpenAIAutoResetState struct {
	Status         string `json:"status"` // checking / available / resetting / success / no_credit / failed
	TriggerWindow  string `json:"trigger_window"`
	AvailableCount int    `json:"available_count"`
	CheckedAt      string `json:"checked_at"`
	LastResultAt   string `json:"last_result_at"`
	ErrorCode      string `json:"error_code"`
}

// ParseOpenAIAutoResetState 解析账号 extra 中的自动用卡运行态，没有时返回 nil。
func ParseOpenAIAutoResetState(extra map[string]any) *OpenAIAutoResetState {
	raw, ok := extra["codex_auto_reset_credit_state"]
	if !ok || raw == nil {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var out OpenAIAutoResetState
	if err := json.Unmarshal(b, &out); err != nil {
		return nil
	}
	return &out
}
