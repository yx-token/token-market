package buyer

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
)

const (
	helperAuthPrefix = "tm:helperauth:"
	helperAuthTTL    = 10 * time.Minute
)

// HelperAuthRecord 闲猫助手「浏览器登录」设备码（Cursor / Claude Code 同款：开浏览器授权，助手轮询）。
type HelperAuthRecord struct {
	Code      string `json:"code"`
	Status    string `json:"status"` // pending | authorized
	UserID    int64  `json:"user_id,omitempty"`
	Account   string `json:"account,omitempty"`
	APIBase   string `json:"api_base"`
	OS        string `json:"os"`
	Ticket    string `json:"ticket,omitempty"` // 授权成功后附带的一次性换票凭证
	CreatedAt int64  `json:"created_at"`
}

// HelperAuthStatus 给助手轮询 / 网页展示用的脱敏视图。
type HelperAuthStatus struct {
	Status    string `json:"status"`
	ExpiresIn int    `json:"expires_in"`
	UserID    int64  `json:"user_id,omitempty"`
	Account   string `json:"account,omitempty"`
	APIBase   string `json:"api_base,omitempty"`
	Ticket    string `json:"ticket,omitempty"`
	OS        string `json:"os,omitempty"`
	Client    string `json:"client,omitempty"`
}

func helperAuthKey(code string) string { return helperAuthPrefix + code }

func normalizeHelperAuthCode(code string) (string, error) {
	code = strings.TrimSpace(strings.ToLower(code))
	if len(code) < 16 || len(code) > 64 {
		return "", httpx.BadRequest("INVALID_DEVICE_CODE", "invalid device code")
	}
	for _, c := range code {
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') {
			continue
		}
		return "", httpx.BadRequest("INVALID_DEVICE_CODE", "invalid device code")
	}
	return code, nil
}

// StartHelperAuth 助手发起登录：登记 pending 设备码。
func (s *OneClickService) StartHelperAuth(ctx context.Context, code, apiBase, osName string) (*HelperAuthStatus, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	code, err := normalizeHelperAuthCode(code)
	if err != nil {
		return nil, err
	}
	apiBase, err = normalizeAPIBase(apiBase)
	if err != nil {
		return nil, err
	}
	if osName == "" {
		osName = "macos"
	}
	osName, err = normalizeOneClickOS(osName)
	if err != nil {
		return nil, err
	}
	rec := HelperAuthRecord{
		Code:      code,
		Status:    "pending",
		APIBase:   apiBase,
		OS:        osName,
		CreatedAt: time.Now().Unix(),
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		return nil, err
	}
	if err := s.Redis.Set(ctx, helperAuthKey(code), raw, helperAuthTTL).Err(); err != nil {
		return nil, err
	}
	return &HelperAuthStatus{
		Status:    "pending",
		ExpiresIn: int(helperAuthTTL.Seconds()),
		APIBase:   apiBase,
		OS:        osName,
	}, nil
}

func (s *OneClickService) loadHelperAuth(ctx context.Context, code string) (*HelperAuthRecord, error) {
	code, err := normalizeHelperAuthCode(code)
	if err != nil {
		return nil, err
	}
	raw, err := s.Redis.Get(ctx, helperAuthKey(code)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, httpx.NotFound("DEVICE_CODE_EXPIRED", "device code expired or unknown")
	}
	if err != nil {
		return nil, err
	}
	var rec HelperAuthRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return nil, httpx.NotFound("DEVICE_CODE_EXPIRED", "device code corrupted")
	}
	return &rec, nil
}

// PollHelperAuth 助手轮询；authorized 时附带一次性 ticket（读后仍保留到 TTL，ticket 本身 GetDel 换票）。
func (s *OneClickService) PollHelperAuth(ctx context.Context, code string) (*HelperAuthStatus, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	rec, err := s.loadHelperAuth(ctx, code)
	if err != nil {
		return nil, err
	}
	ttl, _ := s.Redis.TTL(ctx, helperAuthKey(rec.Code)).Result()
	exp := int(ttl.Seconds())
	if exp < 0 {
		exp = 0
	}
	out := &HelperAuthStatus{
		Status:    rec.Status,
		ExpiresIn: exp,
		APIBase:   rec.APIBase,
		OS:        rec.OS,
		Client:    "helper",
	}
	if rec.Status == "authorized" {
		out.UserID = rec.UserID
		out.Account = rec.Account
		out.Ticket = rec.Ticket
	}
	return out, nil
}

// AuthorizeHelperAuth 网站已登录用户确认授权：绑定账号并签发一键换票 ticket。
func (s *OneClickService) AuthorizeHelperAuth(ctx context.Context, userID int64, code string) (*HelperAuthStatus, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if userID <= 0 {
		return nil, httpx.Unauthorized("UNAUTHORIZED", "login required")
	}
	rec, err := s.loadHelperAuth(ctx, code)
	if err != nil {
		return nil, err
	}
	if rec.Status == "authorized" && rec.UserID == userID {
		return s.PollHelperAuth(ctx, code)
	}
	if rec.Status == "authorized" {
		return nil, httpx.BadRequest("DEVICE_CODE_USED", "device code already authorized by another account")
	}

	account := ""
	if s.Keys != nil && s.Keys.DB != nil {
		_ = s.Keys.DB.QueryRow(ctx, `SELECT COALESCE(NULLIF(TRIM(email), ''), '') FROM users WHERE id = $1`, userID).Scan(&account)
	}

	issued, err := s.Issue(ctx, userID, IssueOneClickInput{
		Client:  "helper",
		OS:      rec.OS,
		APIBase: rec.APIBase,
	})
	if err != nil {
		return nil, err
	}

	rec.Status = "authorized"
	rec.UserID = userID
	rec.Account = strings.TrimSpace(account)
	rec.Ticket = issued.Ticket
	raw, err := json.Marshal(rec)
	if err != nil {
		return nil, err
	}
	ttl, _ := s.Redis.TTL(ctx, helperAuthKey(rec.Code)).Result()
	if ttl <= 0 {
		ttl = helperAuthTTL
	}
	if err := s.Redis.Set(ctx, helperAuthKey(rec.Code), raw, ttl).Err(); err != nil {
		return nil, err
	}
	return &HelperAuthStatus{
		Status:    "authorized",
		ExpiresIn: int(ttl.Seconds()),
		UserID:    userID,
		Account:   rec.Account,
		APIBase:   rec.APIBase,
		Ticket:    issued.Ticket,
		OS:        rec.OS,
		Client:    "helper",
	}, nil
}
