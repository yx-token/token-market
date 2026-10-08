package adminclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Platform    string `json:"platform"`
	Type        string `json:"type"`
	Status      string `json:"status"`
	Schedulable bool   `json:"schedulable"`
	Priority    int    `json:"priority"`
	Concurrency int    `json:"concurrency"`
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
// （只保留配额用量等受保护键），修改部分 extra 键请用 BulkUpdateAccounts。
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

func (c *Client) DeleteAccount(ctx context.Context, id int64) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/api/v1/admin/accounts/%d", id), nil, "", nil)
}
