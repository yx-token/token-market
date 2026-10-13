package fakesub2api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
)

// 默认目录包含现有端到端用例与各厂商默认检测使用的模型。
var defaultAccountModels = []string{
	"claude-sonnet-4-5", "claude-sonnet-4-5-20250929", "claude-haiku-4-5", "claude-opus-4-7", "claude-opus-4-8",
	"claude-opus-5-5", "claude-sonnet-5", "claude-haiku-5-5", "claude-fable-5-1",
	"gpt-4o", "gpt-5.1", "gpt-6", "gpt-6-astra", "gpt-6-sol", "gpt-6.1-sol",
	"gemini-2.5-pro", "gemini-2.5-flash", "gemini-3-flash", "gemini-3.8-flash", "grok-4", "grok-4.7",
	"kimi-for-coding", "kimi-k2.6", "kimi-k3", "glm-4.6", "MiniMax-M2",
}

// SetAccountModels 设置账号的上游完整目录；账号 0 表示未单独配置的账号的默认目录。
// 显式空数组模拟上游没有可发现的模型，不会回落到替身默认值。
func (s *Server) SetAccountModels(accountID int64, models []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accountModels[accountID] = append([]string{}, models...)
}

// FailAccountModelsSync 模拟同步目录接口失败；0 恢复正常，账号 0 设置默认行为。
func (s *Server) FailAccountModelsSync(accountID int64, status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.modelSyncFailures[accountID] = status
}

// FailAccountModelsQuery 模拟普通目录查询失败；0 恢复正常，账号 0 设置默认行为。
func (s *Server) FailAccountModelsQuery(accountID int64, status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.modelGetFailures[accountID] = status
}

// AccountModelRequests 返回两种目录接口的请求次数，用于断言优先同步与查询回退。
func (s *Server) AccountModelRequests(accountID int64) (syncCount, queryCount int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.modelSyncRequests[accountID], s.modelGetRequests[accountID]
}

func (s *Server) accountModelCatalog(accountID int64, syncUpstream bool) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	failures := s.modelGetFailures
	if syncUpstream {
		s.modelSyncRequests[accountID]++
		failures = s.modelSyncFailures
	} else {
		s.modelGetRequests[accountID]++
	}
	status, configured := failures[accountID]
	if !configured {
		status = failures[0]
	}
	if status != 0 {
		return nil, &apiErr{status, "MODEL_CATALOG_FAILED", "模拟模型目录获取失败"}
	}
	models, configured := s.accountModels[accountID]
	if !configured {
		models, configured = s.accountModels[0]
	}
	if !configured {
		models = defaultAccountModels
	}
	return append([]string{}, models...), nil
}

func (s *Server) syncAccountModels(_ http.ResponseWriter, r *http.Request) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	if _, err := s.getAccount(r, id); err != nil {
		return nil, err
	}
	models, err := s.accountModelCatalog(id, true)
	if err != nil {
		return nil, err
	}
	return map[string]any{"models": models}, nil
}

func (s *Server) getAccountModels(_ http.ResponseWriter, r *http.Request) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	if _, err := s.getAccount(r, id); err != nil {
		return nil, err
	}
	models, err := s.accountModelCatalog(id, false)
	if err != nil {
		return nil, err
	}
	mapping, err := s.AccountModelMapping(id)
	if err != nil {
		return nil, err
	}
	if len(mapping) > 0 {
		// 普通目录接口会受已有映射影响，因此接入时须先缓存完整目录。
		models = make([]string, 0, len(mapping))
		for model := range mapping {
			models = append(models, model)
		}
		sort.Strings(models)
	}
	items := make([]map[string]string, 0, len(models))
	for _, model := range models {
		items = append(items, map[string]string{"id": model, "object": "model", "type": "model", "display_name": model})
	}
	return items, nil
}

// AccountModelMapping 返回账号当前的模型映射，用于断言白名单已落入 sub2api。
func (s *Server) AccountModelMapping(accountID int64) (map[string]string, error) {
	credentials, err := s.AccountCredentials(accountID)
	if err != nil {
		return nil, err
	}
	return accountModelMapping(credentials), nil
}

func accountModelMapping(credentials map[string]any) map[string]string {
	out := map[string]string{}
	switch mapping := credentials["model_mapping"].(type) {
	case map[string]any:
		for model, target := range mapping {
			if target, ok := target.(string); ok {
				out[model] = target
			}
		}
	case map[string]string:
		for model, target := range mapping {
			out[model] = target
		}
	}
	return out
}

// supportsAccountModel 模拟账号级映射的白名单过滤，空映射允许全部模型。
func supportsAccountModel(raw []byte, model string) bool {
	var credentials map[string]any
	if err := json.Unmarshal(raw, &credentials); err != nil {
		return false
	}
	mapping := accountModelMapping(credentials)
	if len(mapping) == 0 {
		return true
	}
	if _, ok := mapping[model]; ok {
		return true
	}
	for pattern := range mapping {
		if wildcardModelMatch(pattern, model) {
			return true
		}
	}
	return false
}

func wildcardModelMatch(pattern, model string) bool {
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 || !strings.HasPrefix(model, parts[0]) {
		return false
	}
	remaining := model[len(parts[0]):]
	for _, part := range parts[1 : len(parts)-1] {
		index := strings.Index(remaining, part)
		if index < 0 {
			return false
		}
		remaining = remaining[index+len(part):]
	}
	return strings.HasSuffix(remaining, parts[len(parts)-1])
}

func mergeSensitiveAccountCredentials(existing, incoming map[string]any) map[string]any {
	out := make(map[string]any, len(existing)+len(incoming))
	for key, value := range incoming {
		out[key] = value
	}
	// 清单对照 sub2api v0.2.14 的 SensitiveCredentialKeys。
	for _, key := range []string{
		"access_token", "refresh_token", "id_token", "agent_private_key", "api_key", "session_key", "cookie",
		"password", "sso_token", "sso", "sso-rw", "clearTextPassword", "aws_secret_access_key", "aws_session_token",
		"service_account_json", "service_account", "private_key",
	} {
		if _, present := incoming[key]; present {
			continue
		}
		if value, present := existing[key]; present {
			out[key] = value
		}
	}
	return out
}
