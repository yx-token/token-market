// Package userclient 以影子用户身份调用 sub2api 的用户接口。
//
// sub2api 的 Admin API 不能替用户创建 API key，所以内部 key 的增删改必须用影子用户的登录态。
// 登录接口限流每分钟 20 次：影子用户只在创建时登录一次，之后用 refresh token 续期。
package userclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/DueGin/token-market/internal/sub2api/apiwire"
)

type Client struct {
	wire *apiwire.Client
}

func New(baseURL string, timeout time.Duration, hc *http.Client) *Client {
	return &Client{wire: apiwire.New(baseURL, timeout, hc)}
}

type Tokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

// ExpiresAt 根据签发时刻计算访问令牌过期时间。
func (t Tokens) ExpiresAt(issuedAt time.Time) time.Time {
	return issuedAt.Add(time.Duration(t.ExpiresIn) * time.Second)
}

var ErrNoRefreshToken = errors.New("userclient: login response has no refresh token")

// Login 用邮箱密码登录。sub2api 必须关闭登录验证码，否则这里会失败。
func (c *Client) Login(ctx context.Context, email, password string) (*Tokens, error) {
	body := struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}{email, password}
	var out Tokens
	if err := c.wire.Do(ctx, http.MethodPost, "/api/v1/auth/login", nil, body, &out); err != nil {
		return nil, err
	}
	if out.AccessToken == "" {
		return nil, errors.New("userclient: login response has no access token (2FA or captcha enabled?)")
	}
	if out.RefreshToken == "" {
		return nil, ErrNoRefreshToken
	}
	return &out, nil
}

// Refresh 用 refresh token 换新的令牌对。sub2api 返回新的 refresh token，调用方必须保存新值（旧值可能失效）。
func (c *Client) Refresh(ctx context.Context, refreshToken string) (*Tokens, error) {
	body := struct {
		RefreshToken string `json:"refresh_token"`
	}{refreshToken}
	var out Tokens
	if err := c.wire.Do(ctx, http.MethodPost, "/api/v1/auth/refresh", nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

type APIKey struct {
	ID      int64  `json:"id"`
	UserID  int64  `json:"user_id"`
	Key     string `json:"key"`
	Name    string `json:"name"`
	GroupID *int64 `json:"group_id"`
	Status  string `json:"status"`
}

type CreateKeyInput struct {
	Name      string `json:"name"`
	GroupID   int64  `json:"group_id"`
	CustomKey string `json:"custom_key"` // 由本系统生成：≥16 位，仅字母、数字、下划线、连字符
}

// CreateKey 创建内部 key。使用 custom_key 使重试天然幂等：同一 key 已存在时 sub2api 返回冲突，
// 调用方据此按 key 查询已创建的记录。
func (c *Client) CreateKey(ctx context.Context, accessToken string, in CreateKeyInput) (*APIKey, error) {
	if err := ValidateCustomKey(in.CustomKey); err != nil {
		return nil, err
	}
	var out APIKey
	if err := c.wire.Do(ctx, http.MethodPost, "/api/v1/keys", bearer(accessToken), in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

type UpdateKeyInput struct {
	Name    string `json:"name,omitempty"`
	GroupID *int64 `json:"group_id,omitempty"`
	Status  string `json:"status,omitempty"` // active / inactive
}

func (c *Client) UpdateKey(ctx context.Context, accessToken string, id int64, in UpdateKeyInput) (*APIKey, error) {
	var out APIKey
	if err := c.wire.Do(ctx, http.MethodPut, fmt.Sprintf("/api/v1/keys/%d", id), bearer(accessToken), in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteKey(ctx context.Context, accessToken string, id int64) error {
	return c.wire.Do(ctx, http.MethodDelete, fmt.Sprintf("/api/v1/keys/%d", id), bearer(accessToken), nil, nil)
}

func bearer(token string) http.Header {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+token)
	return h
}

// ValidateCustomKey 与 sub2api 的自定义 key 规则保持一致，提前在本地拒绝非法值。
func ValidateCustomKey(key string) error {
	if len(key) < 16 {
		return errors.New("userclient: custom key must be at least 16 characters")
	}
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return fmt.Errorf("userclient: custom key contains invalid character %q", r)
		}
	}
	return nil
}
