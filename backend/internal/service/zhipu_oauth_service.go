package service

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	infraerrors "github.com/yx-token/token-market/internal/pkg/errors"
	"github.com/yx-token/token-market/internal/pkg/zhipu"
)

// ZhipuClientFactory must retain the account's outbound proxy and URL policy.
type ZhipuClientFactory func(proxyURL string, accountID int64, concurrency int) (*zhipu.Client, error)

type ZhipuOAuthService struct {
	sessions  zhipu.OAuthSessionStore
	proxyRepo ProxyRepository
	factory   ZhipuClientFactory
	version   string
}

func NewZhipuOAuthService(proxyRepo ProxyRepository, factory ZhipuClientFactory, sessions zhipu.OAuthSessionStore, version string) *ZhipuOAuthService {
	if version == "" {
		version = zhipu.ClientVersion
	}
	return &ZhipuOAuthService{proxyRepo: proxyRepo, factory: factory, sessions: sessions, version: version}
}

type ZhipuOAuthInitResult struct {
	SessionID    string         `json:"session_id"`
	Provider     zhipu.Provider `json:"provider"`
	AuthorizeURL string         `json:"authorize_url"`
	ExpiresIn    int64          `json:"expires_in"`
	ExpiresAt    int64          `json:"expires_at"`
	Interval     int            `json:"interval"`
}

type ZhipuOAuthResult struct {
	Provider    zhipu.Provider `json:"provider"`
	Credentials map[string]any `json:"credentials"`
	Display     map[string]any `json:"display"`
}

type ZhipuOAuthPollResult struct {
	Status   string            `json:"status"`
	Interval int               `json:"interval,omitempty"`
	Message  string            `json:"message,omitempty"`
	Result   *ZhipuOAuthResult `json:"result,omitempty"`
}

type ZhipuExchangeCodeInput struct {
	SessionID   string `json:"session_id"`
	CallbackURL string `json:"callback_url"`
	Code        string `json:"code"`
	State       string `json:"state"`
	ProxyID     *int64 `json:"proxy_id"`
}

func zhipuOAuthError(status int, code, message string) error {
	return infraerrors.New(status, "ZHIPU_OAUTH_"+code, message)
}

func (s *ZhipuOAuthService) proxyURL(ctx context.Context, id *int64) (string, error) {
	if id == nil {
		return "", nil
	}
	if *id <= 0 || s.proxyRepo == nil {
		return "", zhipuOAuthError(400, "INVALID_PROXY", "代理不可用")
	}
	proxy, err := s.proxyRepo.GetByID(ctx, *id)
	if err != nil || proxy == nil {
		return "", zhipuOAuthError(400, "INVALID_PROXY", "代理不存在或暂不可用")
	}
	return proxy.URL(), nil
}

func (s *ZhipuOAuthService) Init(ctx context.Context, provider zhipu.Provider, proxyID *int64) (*ZhipuOAuthInitResult, error) {
	if provider != zhipu.ProviderBigModel && provider != zhipu.ProviderZai {
		return nil, zhipuOAuthError(400, "INVALID_PROVIDER", "provider 必须是 bigmodel 或 zai")
	}
	if s.sessions == nil || s.factory == nil {
		return nil, zhipuOAuthError(503, "NOT_CONFIGURED", "GLM OAuth 会话存储未配置")
	}
	proxy, err := s.proxyURL(ctx, proxyID)
	if err != nil {
		return nil, err
	}
	client, err := s.factory(proxy, 0, 1)
	if err != nil {
		return nil, err
	}
	pollToken, err := zhipu.GeneratePollToken()
	if err != nil {
		return nil, err
	}
	init, err := client.Init(ctx, provider, pollToken)
	if err != nil {
		return nil, err
	}
	sid, err := zhipu.GeneratePollToken()
	if err != nil {
		return nil, err
	}
	session := &zhipu.OAuthSession{Provider: provider, FlowID: init.FlowID, State: init.State, PollToken: pollToken, ProxyID: proxyID, ProxyURL: proxy, RedirectURI: client.DesktopRedirectURI(), ExpiresAt: init.ExpiresAt, Interval: init.PollIntervalSec, Status: "pending"}
	if err := s.sessions.Create(ctx, sid, session); err != nil {
		return nil, err
	}
	return &ZhipuOAuthInitResult{SessionID: sid, Provider: provider, AuthorizeURL: init.AuthorizeURL, ExpiresIn: int64(time.Until(init.ExpiresAt).Seconds()), ExpiresAt: init.ExpiresAt.Unix(), Interval: init.PollIntervalSec}, nil
}

func (s *ZhipuOAuthService) acquire(ctx context.Context, id string, proxyID *int64) (*zhipu.OAuthSession, string, error) {
	if s.sessions == nil {
		return nil, "", zhipuOAuthError(503, "NOT_CONFIGURED", "GLM OAuth 会话存储未配置")
	}
	owner, err := zhipu.GeneratePollToken()
	if err != nil {
		return nil, "", err
	}
	session, err := s.sessions.Acquire(ctx, strings.TrimSpace(id), owner)
	if err != nil {
		return nil, "", err
	}
	if !time.Now().Before(session.ExpiresAt) {
		return nil, "", zhipu.ErrSessionExpired
	}
	if proxyID != nil && (session.ProxyID == nil || *proxyID != *session.ProxyID) {
		if session.Status == "pending" {
			_ = s.sessions.Save(ctx, id, owner, session)
		}
		return nil, "", zhipuOAuthError(400, "PROXY_MISMATCH", "代理必须与初始化会话时一致")
	}
	return session, owner, nil
}

func (s *ZhipuOAuthService) Poll(ctx context.Context, id string, proxyID *int64) (*ZhipuOAuthPollResult, error) {
	id = strings.TrimSpace(id)
	session, owner, err := s.acquire(ctx, id, proxyID)
	if errors.Is(err, zhipu.ErrSessionExpired) {
		return &ZhipuOAuthPollResult{Status: "expired", Message: "授权会话已过期，请重新授权"}, nil
	}
	if errors.Is(err, zhipu.ErrSessionBusy) {
		return &ZhipuOAuthPollResult{Status: "pending", Interval: 2}, nil
	}
	if err != nil {
		return nil, err
	}
	if session.Status != "pending" {
		return storedZhipuPollResult(session)
	}
	if time.Now().Before(session.NextPollAt) {
		err = s.sessions.Save(ctx, id, owner, session)
		return &ZhipuOAuthPollResult{Status: "pending", Interval: session.Interval}, err
	}
	client, err := s.factory(session.ProxyURL, 0, 1)
	if err != nil {
		_ = s.saveTerminal(ctx, id, owner, session)
		return nil, err
	}
	polled, err := client.Poll(ctx, session.Provider, session.FlowID, session.PollToken)
	if err != nil {
		_ = s.sessions.Save(ctx, id, owner, session)
		return nil, err
	}
	session.NextPollAt = time.Now().Add(time.Duration(session.Interval) * time.Second)
	switch polled.Status {
	case zhipu.PollPending:
		err = s.sessions.Save(ctx, id, owner, session)
		return &ZhipuOAuthPollResult{Status: "pending", Interval: session.Interval}, err
	case zhipu.PollFailed, zhipu.PollExpired:
		session.Status = string(polled.Status)
	case zhipu.PollReady:
		var tokens *zhipu.TokenSet
		tokens, err = client.NormalizeTokens(ctx, session.Provider, *polled.Tokens)
		if err == nil {
			err = s.complete(ctx, client, session, tokens)
		}
	}
	if err != nil {
		session.Status = "failed"
	}
	scrubZhipuSession(session)
	if saveErr := s.saveTerminal(ctx, id, owner, session); saveErr != nil {
		return nil, saveErr
	}
	return storedZhipuPollResult(session)
}

func (s *ZhipuOAuthService) ExchangeCode(ctx context.Context, input ZhipuExchangeCodeInput) (*ZhipuOAuthResult, error) {
	id := strings.TrimSpace(input.SessionID)
	session, owner, err := s.acquire(ctx, id, input.ProxyID)
	if err != nil {
		return nil, err
	}
	if session.Status != "pending" {
		stored, err := storedZhipuPollResult(session)
		if err != nil {
			return nil, err
		}
		if stored.Result != nil {
			return stored.Result, nil
		}
		return nil, zhipuOAuthError(409, "SESSION_ALREADY_USED", "授权会话已被消费，请重新授权")
	}
	raw := input.CallbackURL
	if strings.TrimSpace(raw) == "" {
		raw = url.Values{"code": {input.Code}, "state": {input.State}}.Encode()
	}
	callback, err := zhipu.ParseAuthorizationInput(session.Provider, raw)
	if err != nil {
		err = zhipuOAuthError(400, "INVALID_CALLBACK", "回调格式无效，请粘贴完整回调地址或包含 code 和 state 的参数")
	}
	if err == nil && input.State != "" && input.State != callback.State {
		err = zhipuOAuthError(400, "INVALID_STATE", "回调 state 不一致")
	}
	if err == nil && subtle.ConstantTimeCompare([]byte(callback.State), []byte(session.State)) != 1 {
		err = zhipuOAuthError(400, "INVALID_STATE", "回调 state 与授权会话不一致")
	}
	if err != nil {
		_ = s.saveTerminal(ctx, id, owner, session)
		return nil, err
	}
	client, err := s.factory(session.ProxyURL, 0, 1)
	if err == nil {
		var tokens *zhipu.TokenSet
		tokens, err = client.ExchangeCode(ctx, session.Provider, *callback, session.State, session.RedirectURI)
		if err == nil {
			err = s.complete(ctx, client, session, tokens)
		}
	}
	if err != nil {
		session.Status = "failed"
	}
	scrubZhipuSession(session)
	if saveErr := s.saveTerminal(ctx, id, owner, session); saveErr != nil {
		return nil, saveErr
	}
	if err != nil {
		return nil, err
	}
	stored, err := storedZhipuPollResult(session)
	if err != nil {
		return nil, err
	}
	return stored.Result, nil
}

func scrubZhipuSession(session *zhipu.OAuthSession) {
	session.PollToken = ""
	session.FlowID = ""
	session.State = ""
	session.ProxyURL = ""
	session.RedirectURI = ""
}

func (s *ZhipuOAuthService) saveTerminal(ctx context.Context, id, owner string, session *zhipu.OAuthSession) error {
	// Complete consumption even when the caller disconnects after token exchange.
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	return s.sessions.Save(cleanup, id, owner, session)
}

func storedZhipuPollResult(session *zhipu.OAuthSession) (*ZhipuOAuthPollResult, error) {
	result := &ZhipuOAuthPollResult{Status: session.Status}
	if session.Status == "success" {
		var value ZhipuOAuthResult
		if err := json.Unmarshal(session.Result, &value); err != nil {
			return nil, err
		}
		result.Result = &value
	}
	if session.Status == "failed" {
		result.Message = "授权或派生密钥失败，请重新授权"
	}
	return result, nil
}

func (s *ZhipuOAuthService) complete(ctx context.Context, client *zhipu.Client, session *zhipu.OAuthSession, tokens *zhipu.TokenSet) error {
	key, err := client.DeriveKey(ctx, session.Provider, tokens.AccessToken)
	if err != nil {
		return err
	}
	result := s.BuildResult(tokens, key)
	session.Result, err = json.Marshal(result)
	if err == nil {
		session.Status = "success"
	}
	return err
}

// BuildResult returns a complete OAuth lifecycle snapshot. Explicit null values
// clear credentials from a previous authorization through the normal admin merge.
// No code, poll token, nonce or private key is persisted.
func (s *ZhipuOAuthService) BuildResult(tokens *zhipu.TokenSet, key *zhipu.DerivedKey) *ZhipuOAuthResult {
	creds := map[string]any{
		"api_key": key.Credential, "access_token": tokens.AccessToken, "zcode_jwt": tokens.ZCodeJWT,
		"oauth_provider": string(tokens.Provider), "zhipu_organization": key.OrganizationID,
		"zhipu_project": key.ProjectID, "client_version": s.version,
		"refresh_token": nil, "zai_oauth_access_token": nil,
		"business_expiry_unknown": tokens.Provider == zhipu.ProviderZai && tokens.BusinessExpiresAt.IsZero(),
	}
	if tokens.RefreshToken != "" {
		creds["refresh_token"] = tokens.RefreshToken
	}
	if tokens.Provider == zhipu.ProviderZai {
		creds["zai_oauth_access_token"] = tokens.OAuthAccessToken
	}
	for name, exp := range map[string]time.Time{"zcode_jwt_expires_at": tokens.JWTExpiresAt, "business_expires_at": tokens.BusinessExpiresAt, "oauth_expires_at": tokens.OAuthExpiresAt} {
		creds[name] = nil
		if !exp.IsZero() {
			creds[name] = exp.UTC().Format(time.RFC3339)
		}
	}
	user := map[string]any{}
	display := map[string]any{}
	for _, name := range []string{"id", "user_id", "sub", "email", "username", "name", "displayName"} {
		if value, ok := tokens.User[name].(string); ok && value != "" {
			user[name] = value
		}
	}
	creds["user_info"] = user
	for _, name := range []string{"email", "username"} {
		if value, ok := user[name]; ok {
			display[name] = value
		}
	}
	if !tokens.JWTExpiresAt.IsZero() {
		display["expires_at"] = tokens.JWTExpiresAt.Unix()
	}
	return &ZhipuOAuthResult{Provider: tokens.Provider, Credentials: creds, Display: display}
}
