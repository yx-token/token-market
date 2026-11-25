package buyer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
	"github.com/yx-token/token-market/market/internal/scheduling"
)

const (
	oneClickTicketPrefix = "tm:oneclick:"
	oneClickTicketTTL    = 5 * time.Minute
)

// OneClickService 签发短时一次性 ticket，供「闲猫助手」换取 Key 明文（协议里不带密钥）。
type OneClickService struct {
	Redis   *redis.Client
	Keys    *KeyService
	EdgeURL string
}

type OneClickTicketRecord struct {
	UserID   int64  `json:"user_id"`
	KeyID    int64  `json:"key_id"`
	Client   string `json:"client"`
	OS       string `json:"os"`
	APIBase  string `json:"api_base"`
	IssuedAt int64  `json:"issued_at"`
}

type IssueOneClickInput struct {
	KeyID    int64
	Client   string
	OS       string
	APIBase  string // 助手换票时回连的市场根地址（含 scheme）
	Platform string // 可选：保障池厂商；空则按客户端默认（多厂商默认 openai）
}

type IssuedOneClickTicket struct {
	Ticket    string `json:"ticket"`
	Deeplink  string `json:"deeplink"`
	ExpiresIn int    `json:"expires_in"`
	APIBase   string `json:"api_base"`
}

type ExchangedOneClick struct {
	Key       string `json:"key"`
	KeyID     int64  `json:"key_id"`
	KeyName   string `json:"key_name"`
	Platform  string `json:"platform,omitempty"`
	BaseURL   string `json:"base_url"`
	Client    string `json:"client"`
	OS        string `json:"os"`
	Provider  string `json:"provider"`
	UserID    int64  `json:"user_id,omitempty"`
	Account   string `json:"account,omitempty"` // 登录邮箱，供助手展示「已关联账号」
	ExpiresIn int    `json:"expires_in"`        // 已消费，恒为 0；保留字段兼容助手
}

func (s *OneClickService) ready() error {
	if s == nil || s.Redis == nil || s.Keys == nil {
		return httpx.BadRequest("ONECLICK_UNAVAILABLE", "one-click helper is not configured")
	}
	return nil
}

func normalizeOneClickOS(v string) (string, error) {
	switch strings.TrimSpace(strings.ToLower(v)) {
	case "macos", "windows", "linux":
		return strings.TrimSpace(strings.ToLower(v)), nil
	default:
		return "", httpx.BadRequest("INVALID_OS", "os must be macos, windows, or linux")
	}
}

func normalizeAPIBase(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", httpx.BadRequest("INVALID_API_BASE", "api_base is required")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", httpx.BadRequest("INVALID_API_BASE", "api_base must be an absolute http(s) URL")
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return "", httpx.BadRequest("INVALID_API_BASE", "api_base must use http or https")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func isLoopbackHTTPURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

// pickExchangeBaseURL 选择写入客户端配置的 API 根地址。
// ticketAPI 是发票时浏览器/助手实际使用的站点；edge 是服务配置的对外网关。
// 当 edge 是本机回环而 ticket 是局域网/公网地址时，必须用 ticket，否则异机会连错。
func pickExchangeBaseURL(edge, ticketAPI string) string {
	edge = strings.TrimRight(strings.TrimSpace(edge), "/")
	ticketAPI = strings.TrimRight(strings.TrimSpace(ticketAPI), "/")
	switch {
	case ticketAPI != "" && (edge == "" || isLoopbackHTTPURL(edge)):
		return ticketAPI
	case edge != "" && (ticketAPI == "" || isLoopbackHTTPURL(ticketAPI)):
		return edge
	case ticketAPI != "":
		return ticketAPI
	default:
		return edge
	}
}

func newOneClickToken() (string, error) {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func pickGuaranteePool(pools []scheduling.Pool, wantPlatform string) (*scheduling.Pool, error) {
	var candidates []scheduling.Pool
	for _, p := range pools {
		if !p.Enabled || p.Sub2APIGroupID == nil {
			continue
		}
		if p.MarketPublished {
			candidates = append(candidates, p)
		}
	}
	if len(candidates) == 0 {
		for _, p := range pools {
			if p.Enabled && p.Sub2APIGroupID != nil {
				candidates = append(candidates, p)
			}
		}
	}
	if len(candidates) == 0 {
		return nil, httpx.BadRequest("NO_POOL", "no guarantee pool available")
	}
	for i := range candidates {
		if candidates[i].Platform == wantPlatform {
			return &candidates[i], nil
		}
	}
	for i := range candidates {
		if candidates[i].Platform == "composite" {
			return &candidates[i], nil
		}
	}
	return &candidates[0], nil
}

// ResolveOrCreateOneClickKey 按厂商复用已有可恢复明文的保障池密钥；没有则自动创建。
// platform 为空时用客户端默认厂商（多厂商默认 openai / ChatGPT）。
// 不跨厂商复用（避免把 Claude 密钥写进 ChatGPT 客户端）。
func (s *KeyService) ResolveOrCreateOneClickKey(ctx context.Context, userID int64, client, platform string) (*BuyerKey, error) {
	if s == nil {
		return nil, httpx.BadRequest("ONECLICK_UNAVAILABLE", "key service unavailable")
	}
	want, err := ValidateAgentPlatform(client, platform)
	if err != nil {
		return nil, err
	}
	keys, err := s.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	for i := range keys {
		k := &keys[i]
		if k.Status != "active" || !k.Recoverable {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(k.Platform), want) {
			return k, nil
		}
	}

	pools, err := scheduling.ListPools(ctx, s.DB, true)
	if err != nil {
		return nil, err
	}
	pool, err := pickGuaranteePool(pools, want)
	if err != nil {
		return nil, err
	}
	name := "一键使用"
	if pool.MarketTitle != "" {
		name = pool.MarketTitle
	}
	_, k, err := s.Create(ctx, userID, KeyInput{
		Name:   name,
		Route:  RouteInput{Type: "default", Pool: pool.Name},
		Limits: Limits{},
	})
	if err != nil {
		return nil, err
	}
	return k, nil
}

// Issue 校验买家拥有该 Key 后签发 ticket，并返回 xianmao:// deeplink。
// key_id 可省略（≤0）：自动选用密钥列表，没有则创建保障池密钥。
func (s *OneClickService) Issue(ctx context.Context, userID int64, in IssueOneClickInput) (*IssuedOneClickTicket, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	client, err := normalizeOneClickClient(in.Client)
	if err != nil {
		return nil, err
	}
	osName, err := normalizeOneClickOS(in.OS)
	if err != nil {
		return nil, err
	}
	apiBase, err := normalizeAPIBase(in.APIBase)
	if err != nil {
		return nil, err
	}
	platform, err := ValidateAgentPlatform(client, in.Platform)
	if err != nil {
		return nil, err
	}
	keyID := in.KeyID
	if keyID > 0 {
		k, err := s.Keys.Get(ctx, userID, keyID)
		if err != nil {
			return nil, err
		}
		if k.Status != "active" || !k.Recoverable {
			keyID = 0 // 不可用则改走自动解析/创建
		}
	}
	if keyID <= 0 {
		k, err := s.Keys.ResolveOrCreateOneClickKey(ctx, userID, client, platform)
		if err != nil {
			return nil, err
		}
		keyID = k.ID
	}
	token, err := newOneClickToken()
	if err != nil {
		return nil, err
	}
	rec := OneClickTicketRecord{
		UserID:   userID,
		KeyID:    keyID,
		Client:   client,
		OS:       osName,
		APIBase:  apiBase,
		IssuedAt: time.Now().Unix(),
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		return nil, err
	}
	if err := s.Redis.Set(ctx, oneClickTicketPrefix+token, raw, oneClickTicketTTL).Err(); err != nil {
		return nil, err
	}
	q := url.Values{
		"ticket": {token},
		"client": {client},
		"os":     {osName},
		"api":    {apiBase},
	}
	return &IssuedOneClickTicket{
		Ticket:    token,
		Deeplink:  "xianmao://one-click?" + q.Encode(),
		ExpiresIn: int(oneClickTicketTTL.Seconds()),
		APIBase:   apiBase,
	}, nil
}

// Exchange 一次性消费 ticket，返回 Key 明文与助手所需字段。
func (s *OneClickService) Exchange(ctx context.Context, ticket string) (*ExchangedOneClick, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	ticket = strings.TrimSpace(ticket)
	if ticket == "" || len(ticket) > 128 {
		return nil, httpx.BadRequest("INVALID_TICKET", "ticket is required")
	}
	key := oneClickTicketPrefix + ticket
	raw, err := s.Redis.GetDel(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, httpx.Unauthorized("TICKET_INVALID", "ticket expired or already used")
	}
	if err != nil {
		return nil, err
	}
	var rec OneClickTicketRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return nil, httpx.Unauthorized("TICKET_INVALID", "ticket payload corrupted")
	}
	plain, err := s.Keys.Secret(ctx, rec.UserID, rec.KeyID)
	if err != nil {
		return nil, err
	}
	detail, err := s.Keys.Get(ctx, rec.UserID, rec.KeyID)
	if err != nil {
		return nil, err
	}
	// 写入客户端的 base_url 必须是用户实际可达的地址。
	// 局域网访问时 ticket.APIBase 是 http://192.168.x.x:8080，而 EdgeURL 常是 127.0.0.1，
	// 若优先 EdgeURL，Windows 上助手/客户端会连到自己机器而失败。
	baseURL := pickExchangeBaseURL(s.EdgeURL, rec.APIBase)
	account := ""
	if s.Keys != nil && s.Keys.DB != nil {
		// users 表无 deleted_at；带该条件会导致查询失败、账号一直为空
		_ = s.Keys.DB.QueryRow(ctx, `SELECT COALESCE(NULLIF(TRIM(email), ''), '') FROM users WHERE id = $1`, rec.UserID).Scan(&account)
	}
	return &ExchangedOneClick{
		Key:      plain,
		KeyID:    rec.KeyID,
		KeyName:  detail.Name,
		Platform: detail.Platform,
		BaseURL:  baseURL,
		Client:   rec.Client,
		OS:       rec.OS,
		Provider: "闲猫",
		UserID:   rec.UserID,
		Account:  strings.TrimSpace(account),
	}, nil
}

// HelperManifest 返回助手安装包入口（本站同源下载，由网页自动拉起）。
func HelperManifest(version string) map[string]any {
	if version == "" {
		version = "0.1.0"
	}
	return map[string]any{
		"name":        "闲猫助手",
		"version":     version,
		"protocol":    "xianmao",
		"description": "打开闲猫助手后，在助手内选择客户端并自动配置。",
		"downloads": map[string]any{
			"macos": map[string]string{
				"label": "macOS",
				"url":   "/api/one-click/helper/download/macos",
				"hint":  "macOS 11+（Intel / Apple Silicon）",
			},
			"windows": map[string]string{
				"label": "Windows",
				"url":   "/api/one-click/helper/download/windows",
				"hint":  "Windows 10/11 x64",
			},
			"linux": map[string]string{
				"label": "Linux",
				"url":   "/api/one-click/helper/download/linux",
				"hint":  "Linux x64 AppImage（可选）",
			},
		},
	}
}

// BuildDeeplink 供测试与前端拼装校验（不含密钥）。
func BuildDeeplink(ticket, client, osName, apiBase string) (string, error) {
	client, err := normalizeOneClickClient(client)
	if err != nil {
		return "", err
	}
	osName, err = normalizeOneClickOS(osName)
	if err != nil {
		return "", err
	}
	apiBase, err = normalizeAPIBase(apiBase)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(ticket) == "" {
		return "", fmt.Errorf("empty ticket")
	}
	q := url.Values{
		"ticket": {ticket},
		"client": {client},
		"os":     {osName},
		"api":    {apiBase},
	}
	return "xianmao://one-click?" + q.Encode(), nil
}
