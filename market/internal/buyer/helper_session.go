package buyer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
)

const (
	helperSessionPrefix    = "tm:helpersession:"
	helperSessionTTL       = 30 * 24 * time.Hour
	helperSessionPrefixTok = "hs-"
)

// HelperSessionRecord 浏览器登录后下发的长会话：与买家 API Key 解耦，
// 即使网页上轮换/删除了旧 tm- key，助手仍可用会话按厂商重新取密钥。
type HelperSessionRecord struct {
	UserID    int64  `json:"user_id"`
	Account   string `json:"account,omitempty"`
	CreatedAt int64  `json:"created_at"`
}

func helperSessionKey(token string) string { return helperSessionPrefix + token }

func IsHelperSessionToken(token string) bool {
	return strings.HasPrefix(strings.TrimSpace(token), helperSessionPrefixTok)
}

// IssueHelperSession 签发助手长会话（30 天）。
func (s *OneClickService) IssueHelperSession(ctx context.Context, userID int64, account string) (string, error) {
	if err := s.ready(); err != nil {
		return "", err
	}
	if userID <= 0 {
		return "", httpx.Unauthorized("UNAUTHORIZED", "login required")
	}
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := helperSessionPrefixTok + hex.EncodeToString(raw)
	rec := HelperSessionRecord{
		UserID:    userID,
		Account:   strings.TrimSpace(account),
		CreatedAt: time.Now().Unix(),
	}
	body, err := json.Marshal(rec)
	if err != nil {
		return "", err
	}
	if err := s.Redis.Set(ctx, helperSessionKey(token), body, helperSessionTTL).Err(); err != nil {
		return "", err
	}
	return token, nil
}

// LookupHelperSession 校验助手会话并返回用户。
func (s *OneClickService) LookupHelperSession(ctx context.Context, token string) (userID int64, account string, err error) {
	if err := s.ready(); err != nil {
		return 0, "", err
	}
	token = strings.TrimSpace(token)
	if !IsHelperSessionToken(token) || len(token) > 128 {
		return 0, "", httpx.Unauthorized("SESSION_INVALID", "helper session invalid")
	}
	raw, err := s.Redis.Get(ctx, helperSessionKey(token)).Bytes()
	if errors.Is(err, redis.Nil) {
		return 0, "", httpx.Unauthorized("SESSION_INVALID", "helper session expired")
	}
	if err != nil {
		return 0, "", err
	}
	var rec HelperSessionRecord
	if err := json.Unmarshal(raw, &rec); err != nil || rec.UserID <= 0 {
		return 0, "", httpx.Unauthorized("SESSION_INVALID", "helper session corrupted")
	}
	// 滑动续期：活跃使用时延长
	_ = s.Redis.Expire(ctx, helperSessionKey(token), helperSessionTTL).Err()
	return rec.UserID, rec.Account, nil
}

// RevokeHelperSession 退出登录时作废会话。
func (s *OneClickService) RevokeHelperSession(ctx context.Context, token string) {
	if s == nil || s.Redis == nil {
		return
	}
	token = strings.TrimSpace(token)
	if !IsHelperSessionToken(token) {
		return
	}
	_ = s.Redis.Del(ctx, helperSessionKey(token)).Err()
}
