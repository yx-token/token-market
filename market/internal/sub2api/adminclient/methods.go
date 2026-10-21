package adminclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// ErrIdempotencyKeyRequired：sub2api 对创建账号、创建代理、调整余额强制要求 Idempotency-Key。
var ErrIdempotencyKeyRequired = errors.New("adminclient: idempotency key is required for this operation")

type User struct {
	ID          int64   `json:"id"`
	Email       string  `json:"email"`
	Balance     float64 `json:"balance"`
	Concurrency int     `json:"concurrency"`
	Status      string  `json:"status"`
}

type Group struct {
	ID             int64   `json:"id"`
	Name           string  `json:"name"`
	Platform       string  `json:"platform"`
	RateMultiplier float64 `json:"rate_multiplier"`
	IsExclusive    bool    `json:"is_exclusive"`
}

type Account struct {
	ID          int64          `json:"id"`
	Name        string         `json:"name"`
	Platform    string         `json:"platform"`
	Type        string         `json:"type"`
	Status      string         `json:"status"`
	Schedulable bool           `json:"schedulable"`
	Priority    int            `json:"priority"`
	Concurrency int            `json:"concurrency"`
	Extra       map[string]any `json:"extra"`
}

type Proxy struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// ---- 系统 ----

// GetVersion 返回 sub2api 当前版本号。
func (c *Client) GetVersion(ctx context.Context) (string, error) {
	var out struct {
		Version string `json:"version"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v1/admin/system/version", nil, "", &out); err != nil {
		return "", err
	}
	return out.Version, nil
}

// ---- 用户 ----

type CreateUserInput struct {
	Email                string   `json:"email"`
	Password             string   `json:"password"`
	Username             string   `json:"username,omitempty"`
	Notes                string   `json:"notes,omitempty"`
	Balance              *float64 `json:"balance,omitempty"`
	Concurrency          int      `json:"concurrency,omitempty"`
	RPMLimit             int      `json:"rpm_limit,omitempty"`
	AllowedGroups        []int64  `json:"allowed_groups,omitempty"`
	RestrictPublicGroups bool     `json:"restrict_public_groups,omitempty"`
}

// CreateUser 创建用户。sub2api 该接口不支持幂等键，调用方应在重试前按邮箱查询是否已创建。
func (c *Client) CreateUser(ctx context.Context, in CreateUserInput) (*User, error) {
	var out User
	if err := c.do(ctx, http.MethodPost, "/api/v1/admin/users", in, "", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

type UpdateUserInput struct {
	Notes                *string  `json:"notes,omitempty"`
	Concurrency          *int     `json:"concurrency,omitempty"`
	RPMLimit             *int     `json:"rpm_limit,omitempty"`
	Status               string   `json:"status,omitempty"`
	AllowedGroups        *[]int64 `json:"allowed_groups,omitempty"`
	RestrictPublicGroups *bool    `json:"restrict_public_groups,omitempty"`
}

func (c *Client) UpdateUser(ctx context.Context, id int64, in UpdateUserInput) (*User, error) {
	var out User
	if err := c.do(ctx, http.MethodPut, fmt.Sprintf("/api/v1/admin/users/%d", id), in, "", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

type BalanceOperation string

const (
	BalanceAdd      BalanceOperation = "add"
	BalanceSubtract BalanceOperation = "subtract"
)

// UpdateBalance 原子地增减用户余额。sub2api 要求幂等键（有效期 24 小时）；
// subtract 不会把余额扣成负数，余额不足时返回错误。
func (c *Client) UpdateBalance(ctx context.Context, userID int64, op BalanceOperation, amount float64, notes, idempotencyKey string) (*User, error) {
	if idempotencyKey == "" {
		return nil, ErrIdempotencyKeyRequired
	}
	if amount <= 0 {
		return nil, fmt.Errorf("adminclient: balance amount must be positive, got %v", amount)
	}
	body := struct {
		Balance   float64          `json:"balance"`
		Operation BalanceOperation `json:"operation"`
		Notes     string           `json:"notes,omitempty"`
	}{amount, op, notes}
	var out User
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/admin/users/%d/balance", userID), body, idempotencyKey, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ---- 分组 ----

type CreateGroupInput struct {
	Name                 string   `json:"name"`
	Description          string   `json:"description,omitempty"`
	Platform             string   `json:"platform"`
	RateMultiplier       float64  `json:"rate_multiplier"`
	IsExclusive          bool     `json:"is_exclusive"`
	ProfitControlEnabled bool     `json:"profit_control_enabled,omitempty"`
	ProfitMinMargin      *float64 `json:"profit_min_margin,omitempty"`
	ProfitSafetyBuffer   *float64 `json:"profit_safety_buffer,omitempty"`
}

// CreateGroup 创建分组。sub2api 该接口不支持幂等键，调用方应在重试前按名称查询是否已创建。
func (c *Client) CreateGroup(ctx context.Context, in CreateGroupInput) (*Group, error) {
	var out Group
	if err := c.do(ctx, http.MethodPost, "/api/v1/admin/groups", in, "", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

type UpdateGroupInput struct {
	Description          *string  `json:"description,omitempty"`
	RateMultiplier       *float64 `json:"rate_multiplier,omitempty"`
	IsExclusive          *bool    `json:"is_exclusive,omitempty"`
	ProfitControlEnabled *bool    `json:"profit_control_enabled,omitempty"`
	ProfitMinMargin      *float64 `json:"profit_min_margin,omitempty"`
	ProfitSafetyBuffer   *float64 `json:"profit_safety_buffer,omitempty"`
}

func (c *Client) UpdateGroup(ctx context.Context, id int64, in UpdateGroupInput) (*Group, error) {
	var out Group
	if err := c.do(ctx, http.MethodPut, fmt.Sprintf("/api/v1/admin/groups/%d", id), in, "", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteGroup(ctx context.Context, id int64) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/api/v1/admin/groups/%d", id), nil, "", nil)
}

// ---- 代理 ----

type CreateProxyInput struct {
	Name     string `json:"name"`
	Protocol string `json:"protocol"` // http / https / socks5 / socks5h
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

func (c *Client) CreateProxy(ctx context.Context, in CreateProxyInput, idempotencyKey string) (*Proxy, error) {
	if idempotencyKey == "" {
		return nil, ErrIdempotencyKeyRequired
	}
	var out Proxy
	if err := c.do(ctx, http.MethodPost, "/api/v1/admin/proxies", in, idempotencyKey, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteProxy(ctx context.Context, id int64) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/api/v1/admin/proxies/%d", id), nil, "", nil)
}

// ---- 账号 ----

type CreateAccountInput struct {
	Name           string         `json:"name"`
	Notes          *string        `json:"notes,omitempty"`
	Platform       string         `json:"platform"`
	Type           string         `json:"type"`
	Credentials    map[string]any `json:"credentials"`
	Extra          map[string]any `json:"extra,omitempty"`
	ProxyID        *int64         `json:"proxy_id,omitempty"`
	Concurrency    int            `json:"concurrency"`
	Priority       int            `json:"priority"`
	RateMultiplier *float64       `json:"rate_multiplier,omitempty"`
	LoadFactor     *int           `json:"load_factor,omitempty"`
	GroupIDs       []int64        `json:"group_ids"`
}

func (c *Client) CreateAccount(ctx context.Context, in CreateAccountInput, idempotencyKey string) (*Account, error) {
	if idempotencyKey == "" {
		return nil, ErrIdempotencyKeyRequired
	}
	var out Account
	if err := c.do(ctx, http.MethodPost, "/api/v1/admin/accounts", in, idempotencyKey, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateAccountInput 对应 PUT /admin/accounts/:id。注意：传 Extra 时 sub2api 会整体替换 extra
// （只保留配额用量等受保护键）；Credentials 只保留未提交的敏感凭证，其他键由新对象决定。
// 修改部分 extra 或 credentials 键请用 BulkUpdateAccounts。
type UpdateAccountInput struct {
	Name           string         `json:"name,omitempty"`
	Notes          *string        `json:"notes,omitempty"`
	Credentials    map[string]any `json:"credentials,omitempty"`
	Extra          map[string]any `json:"extra,omitempty"`
	ProxyID        *int64         `json:"proxy_id,omitempty"`
	Concurrency    *int           `json:"concurrency,omitempty"`
	Priority       *int           `json:"priority,omitempty"`
	RateMultiplier *float64       `json:"rate_multiplier,omitempty"`
	LoadFactor     *int           `json:"load_factor,omitempty"`
	Status         string         `json:"status,omitempty"`
	GroupIDs       *[]int64       `json:"group_ids,omitempty"`
}

func (c *Client) UpdateAccount(ctx context.Context, id int64, in UpdateAccountInput) (*Account, error) {
	var out Account
	if err := c.do(ctx, http.MethodPut, fmt.Sprintf("/api/v1/admin/accounts/%d", id), in, "", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// BulkUpdateAccountsInput 把同样的值下发给一批账号；Extra 与 Credentials 按 jsonb 合并写入。
type BulkUpdateAccountsInput struct {
	AccountIDs     []int64        `json:"account_ids"`
	Concurrency    *int           `json:"concurrency,omitempty"`
	Priority       *int           `json:"priority,omitempty"`
	RateMultiplier *float64       `json:"rate_multiplier,omitempty"`
	LoadFactor     *int           `json:"load_factor,omitempty"`
	Schedulable    *bool          `json:"schedulable,omitempty"`
	GroupIDs       *[]int64       `json:"group_ids,omitempty"`
	Credentials    map[string]any `json:"credentials,omitempty"`
	Extra          map[string]any `json:"extra,omitempty"`
}

func (c *Client) BulkUpdateAccounts(ctx context.Context, in BulkUpdateAccountsInput) (json.RawMessage, error) {
	if len(in.AccountIDs) == 0 {
		return nil, errors.New("adminclient: bulk update requires account ids")
	}
	var out json.RawMessage
	if err := c.do(ctx, http.MethodPost, "/api/v1/admin/accounts/bulk-update", in, "", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) SetSchedulable(ctx context.Context, id int64, schedulable bool) error {
	body := struct {
		Schedulable bool `json:"schedulable"`
	}{schedulable}
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/admin/accounts/%d/schedulable", id), body, "", nil)
}

// ClearError 清除账号的错误状态（重新授权后恢复调度）。
func (c *Client) ClearError(ctx context.Context, id int64) error {
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/admin/accounts/%d/clear-error", id), nil, "", nil)
}

func (c *Client) DeleteAccount(ctx context.Context, id int64) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/api/v1/admin/accounts/%d", id), nil, "", nil)
}

// ---- 通用调用与厂商接口 ----

// Post 调用任意 Admin API（厂商授权等结构各异的接口），data 解码到 out。
func (c *Client) Post(ctx context.Context, path string, body any, idempotencyKey string, out any) error {
	return c.do(ctx, http.MethodPost, path, body, idempotencyKey, out)
}

// SetPrivacy 为 OpenAI / Antigravity 的 OAuth 账号关闭训练数据使用，返回更新后的账号（extra.privacy_mode）。
func (c *Client) SetPrivacy(ctx context.Context, id int64) (*Account, error) {
	var out Account
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/admin/accounts/%d/set-privacy", id), nil, "", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

type CNQuotaTier struct {
	Window      string  `json:"window"`
	UsedPercent float64 `json:"used_percent"`
	ResetAt     string  `json:"reset_at"`
}

type CNQuota struct {
	Provider        string        `json:"provider"`
	Success         bool          `json:"success"`
	CredentialValid bool          `json:"credential_valid"`
	Tiers           []CNQuotaTier `json:"tiers"`
	PlanLevel       string        `json:"plan_level"`
	Error           string        `json:"error"`
}

// CNProviderQuota 查询国产 Coding Plan 账号的官方窗口用量，用于上架时确认确实是订阅 key。
func (c *Client) CNProviderQuota(ctx context.Context, id int64) (*CNQuota, error) {
	var out CNQuota
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/admin/cn-providers/accounts/%d/quota", id), nil, "", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UsageWindowStats 是窗口期内经 sub2api 的使用量。
type UsageWindowStats struct {
	Requests int64   `json:"requests"`
	Tokens   int64   `json:"tokens"`
	Cost     float64 `json:"cost"`
}

// UsageWindow 是官方订阅的一个用量窗口（5 小时、7 天等）。
type UsageWindow struct {
	Utilization      float64           `json:"utilization"` // 0–100+
	ResetsAt         *time.Time        `json:"resets_at"`
	RemainingSeconds int               `json:"remaining_seconds"`
	WindowStats      *UsageWindowStats `json:"window_stats"`
	UsedRequests     int64             `json:"used_requests"`
	LimitRequests    int64             `json:"limit_requests"`
}

// AccountUsage 是 sub2api 账号用量接口（GET /admin/accounts/:id/usage）中本系统展示的字段。
type AccountUsage struct {
	Source            string       `json:"source"`
	UpdatedAt         *time.Time   `json:"updated_at"`
	FiveHour          *UsageWindow `json:"five_hour"`
	SevenDay          *UsageWindow `json:"seven_day"`
	SevenDaySonnet    *UsageWindow `json:"seven_day_sonnet"`
	SevenDayFable     *UsageWindow `json:"seven_day_fable"`
	ThirtyDay         *UsageWindow `json:"thirty_day"`
	GeminiSharedDaily *UsageWindow `json:"gemini_shared_daily"`
	GeminiProDaily    *UsageWindow `json:"gemini_pro_daily"`
	GeminiFlashDaily  *UsageWindow `json:"gemini_flash_daily"`
	AntigravityQuota  map[string]struct {
		Utilization int    `json:"utilization"`
		ResetTime   string `json:"reset_time"`
	} `json:"antigravity_quota"`
	SubscriptionTier string `json:"subscription_tier"`
	NeedsReauth      bool   `json:"needs_reauth"`
	IsBanned         bool   `json:"is_banned"`
	NeedsVerify      bool   `json:"needs_verify"`
	ErrorCode        string `json:"error_code"`
	Error            string `json:"error"`
}

// AccountUsage 查询账号的官方订阅窗口用量。passive 读取 sub2api 从响应头采样的快照（不访问上游，仅 Claude 支持）；
// 否则由 sub2api 向官方查询（sub2api 自带缓存）。
func (c *Client) AccountUsage(ctx context.Context, id int64, passive bool) (*AccountUsage, error) {
	source := "active"
	if passive {
		source = "passive"
	}
	var out AccountUsage
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/admin/accounts/%d/usage?source=%s", id, source), nil, "", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ModelPricing 是 sub2api 内置价目表中某个模型的官方单价（美元 / token）。
type ModelPricing struct {
	Found           bool    `json:"found"`
	InputPrice      float64 `json:"input_price"`
	OutputPrice     float64 `json:"output_price"`
	CacheWritePrice float64 `json:"cache_write_price"`
	CacheReadPrice  float64 `json:"cache_read_price"`
}

// GetModelPricing 查询模型的官方价格，倍率以它为基准。
func (c *Client) GetModelPricing(ctx context.Context, model string) (*ModelPricing, error) {
	var out ModelPricing
	if err := c.do(ctx, http.MethodGet, "/api/v1/admin/channels/model-pricing?model="+url.QueryEscape(model), nil, "", &out); err != nil {
		return nil, err
	}
	return &out, nil
}
