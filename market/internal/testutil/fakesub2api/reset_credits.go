package fakesub2api

import (
	"encoding/json"
	"net/http"
	"time"
)

// 重置卡：OpenAI OAuth 与 Claude OAuth 账号可以查询、使用重置卡（对照 sub2api 0.2.14）。
// 张数由测试用 SetResetCredits 设定（未设定时为 DefaultResetCredits）；Codex 的用卡接口与 sub2api 一样不幂等，Claude 的兑换要求幂等键。

// SetResetCredits 设定账号可用的重置卡张数。
func (s *Server) SetResetCredits(accountID int64, n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.credits[accountID] = n
}

// ResetsUsed 返回账号实际消耗的重置卡张数。
func (s *Server) ResetsUsed(accountID int64) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.resets[accountID]
}

// Recovers 返回账号被恢复状态（recover-state）的次数。
func (s *Server) Recovers(accountID int64) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.recovers[accountID]
}

func (s *Server) resetAccount(r *http.Request, platform string) (*accountOut, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	a, err := s.getAccount(r, id)
	if err != nil {
		return nil, err
	}
	if a.Platform != platform || a.Type != "oauth" {
		if platform == "openai" {
			return nil, &apiErr{400, "OPENAI_QUOTA_INVALID_TYPE", "account must be an OpenAI OAuth account"}
		}
		return nil, &apiErr{400, "CLAUDE_RESET_OAUTH_REQUIRED", "Claude reset credits require an OAuth account"}
	}
	return a, nil
}

// creditsOf 返回账号可用的重置卡张数，调用方需持有锁。
func (s *Server) creditsOf(accountID int64) int {
	if n, ok := s.credits[accountID]; ok {
		return n
	}
	return s.DefaultResetCredits
}

// consume 消耗一张卡，没有卡时返回 false。
func (s *Server) consume(accountID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.creditsOf(accountID)
	if n <= 0 {
		return false
	}
	s.credits[accountID] = n - 1
	s.resets[accountID]++
	return true
}

func (s *Server) openAICredits(accountID int64) map[string]any {
	s.mu.Lock()
	n := s.creditsOf(accountID)
	s.mu.Unlock()
	credits := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		credits = append(credits, map[string]any{"expires_at": time.Now().Add(time.Duration(i+7) * 24 * time.Hour).UTC().Format(time.RFC3339)})
	}
	return map[string]any{"available_count": n, "credits": credits}
}

// openAIQuotaRefresh 返回 Codex 用量与重置卡，并把重置卡快照写入账号 extra。
func (s *Server) openAIQuotaRefresh(_ http.ResponseWriter, r *http.Request) (any, error) {
	a, err := s.resetAccount(r, "openai")
	if err != nil {
		return nil, err
	}
	credits := s.openAICredits(a.ID)
	snap, _ := json.Marshal(map[string]any{"codex_reset_credit_snapshot": credits})
	if _, err := s.DB.Exec(r.Context(), "UPDATE accounts SET extra = COALESCE(extra, '{}'::jsonb) || $2::jsonb WHERE id = $1", a.ID, snap); err != nil {
		return nil, err
	}
	return map[string]any{"plan_type": "pro", "rate_limit_reset_credits": credits, "fetched_at": time.Now().Unix(),
		"cache_persisted": true, "credits_cache_persisted": true}, nil
}

func (s *Server) openAIResetQuota(_ http.ResponseWriter, r *http.Request) (any, error) {
	a, err := s.resetAccount(r, "openai")
	if err != nil {
		return nil, err
	}
	if !s.consume(a.ID) {
		return map[string]any{"code": "no_credit", "windows_reset": 0}, nil
	}
	// 与 sub2api 的后续处理一致：恢复账号状态，并把用卡后的重置卡快照写回 extra
	s.markRecovered(r, a.ID)
	credits := s.openAICredits(a.ID)
	snap, _ := json.Marshal(map[string]any{"codex_reset_credit_snapshot": credits})
	if _, err := s.DB.Exec(r.Context(), "UPDATE accounts SET extra = COALESCE(extra, '{}'::jsonb) || $2::jsonb WHERE id = $1", a.ID, snap); err != nil {
		return nil, err
	}
	return map[string]any{"code": "reset", "windows_reset": 2, "cache_refreshed": true, "account_state_recovered": true,
		"quota": map[string]any{"rate_limit_reset_credits": credits}}, nil
}

func (s *Server) claudeCredits(accountID int64) map[string]any {
	s.mu.Lock()
	n := s.creditsOf(accountID)
	s.mu.Unlock()
	credits := []map[string]any{}
	if n > 0 {
		credits = append(credits, map[string]any{"label": "Claude reset", "resets_left": n, "clears": []string{"five_hour"},
			"expires_at": time.Now().Add(30 * 24 * time.Hour).UTC().Format(time.RFC3339), "redeemable": true, "use_requires_limit": true})
	}
	return map[string]any{"eligible": true, "available_count": n, "credits": credits, "fetched_at": time.Now().UTC().Format(time.RFC3339)}
}

func (s *Server) claudeResetCredits(_ http.ResponseWriter, r *http.Request) (any, error) {
	a, err := s.resetAccount(r, "anthropic")
	if err != nil {
		return nil, err
	}
	return s.claudeCredits(a.ID), nil
}

func (s *Server) claudeRedeem(_ http.ResponseWriter, r *http.Request) (any, error) {
	a, err := s.resetAccount(r, "anthropic")
	if err != nil {
		return nil, err
	}
	if !s.consume(a.ID) {
		return nil, &apiErr{409, "CLAUDE_RESET_NOT_AVAILABLE", "no redeemable Claude reset credit"}
	}
	return map[string]any{"outcome": "reset", "cleared": []string{"five_hour"}, "credits": s.claudeCredits(a.ID), "replayed": false}, nil
}

// recoverState 清除账号的错误、限流与临时不可调度状态。
func (s *Server) recoverState(_ http.ResponseWriter, r *http.Request) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	if _, err := s.getAccount(r, id); err != nil {
		return nil, err
	}
	s.markRecovered(r, id)
	return s.getAccount(r, id)
}

func (s *Server) markRecovered(r *http.Request, id int64) {
	_, _ = s.DB.Exec(r.Context(), `UPDATE accounts SET status = CASE WHEN status = 'error' THEN 'active' ELSE status END, error_message = NULL,
		rate_limit_reset_at = NULL, temp_unschedulable_until = NULL, temp_unschedulable_reason = NULL WHERE id = $1`, id)
	s.mu.Lock()
	s.recovers[id]++
	s.mu.Unlock()
}
