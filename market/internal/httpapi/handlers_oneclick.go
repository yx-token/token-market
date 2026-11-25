package httpapi

import (
	"crypto/sha256"
	"net/http"
	"strings"

	"github.com/yx-token/token-market/market/internal/buyer"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
	"github.com/yx-token/token-market/market/internal/scheduling"
)

type issueOneClickBody struct {
	KeyID    int64  `json:"key_id"`
	Client   string `json:"client"`
	OS       string `json:"os"`
	APIBase  string `json:"api_base"`
	Platform string `json:"platform"`
}

type exchangeOneClickBody struct {
	Ticket string `json:"ticket"`
}

type startHelperAuthBody struct {
	Code    string `json:"code"`
	APIBase string `json:"api_base"`
	OS      string `json:"os"`
}

type authorizeHelperAuthBody struct {
	Code string `json:"code"`
}

func requestAPIBase(r *http.Request, fallback string) string {
	apiBase := strings.TrimSpace(fallback)
	if apiBase != "" {
		return apiBase
	}
	scheme := "https"
	if r.TLS == nil {
		if xf := r.Header.Get("X-Forwarded-Proto"); xf != "" {
			scheme = strings.Split(xf, ",")[0]
		} else {
			scheme = "http"
		}
	}
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	return scheme + "://" + strings.TrimSpace(host)
}

func (s *Server) issueOneClickTicket(w http.ResponseWriter, r *http.Request) error {
	if s.OneClick == nil {
		return httpx.BadRequest("ONECLICK_UNAVAILABLE", "one-click helper is not configured")
	}
	var in issueOneClickBody
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	apiBase := requestAPIBase(r, in.APIBase)
	out, err := s.OneClick.Issue(r.Context(), httpx.MustUser(r).UserID, buyer.IssueOneClickInput{
		KeyID: in.KeyID, Client: in.Client, OS: in.OS, APIBase: apiBase, Platform: in.Platform,
	})
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	return ok(w, out)
}

func (s *Server) startHelperAuth(w http.ResponseWriter, r *http.Request) error {
	if s.OneClick == nil {
		return httpx.BadRequest("ONECLICK_UNAVAILABLE", "one-click helper is not configured")
	}
	var in startHelperAuthBody
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	out, err := s.OneClick.StartHelperAuth(r.Context(), in.Code, requestAPIBase(r, in.APIBase), in.OS)
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	return ok(w, out)
}

func (s *Server) pollHelperAuth(w http.ResponseWriter, r *http.Request) error {
	if s.OneClick == nil {
		return httpx.BadRequest("ONECLICK_UNAVAILABLE", "one-click helper is not configured")
	}
	code := r.PathValue("code")
	out, err := s.OneClick.PollHelperAuth(r.Context(), code)
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	return ok(w, out)
}

func (s *Server) authorizeHelperAuth(w http.ResponseWriter, r *http.Request) error {
	if s.OneClick == nil {
		return httpx.BadRequest("ONECLICK_UNAVAILABLE", "one-click helper is not configured")
	}
	var in authorizeHelperAuthBody
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	out, err := s.OneClick.AuthorizeHelperAuth(r.Context(), httpx.MustUser(r).UserID, in.Code)
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	return ok(w, out)
}

func (s *Server) exchangeOneClickTicket(w http.ResponseWriter, r *http.Request) error {
	if s.OneClick == nil {
		return httpx.BadRequest("ONECLICK_UNAVAILABLE", "one-click helper is not configured")
	}
	var in exchangeOneClickBody
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	out, err := s.OneClick.Exchange(r.Context(), in.Ticket)
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	return ok(w, out)
}

func (s *Server) oneClickHelperManifest(w http.ResponseWriter, r *http.Request) error {
	return ok(w, buyer.HelperManifest("0.1.0"))
}

func extractHelperAPIKey(r *http.Request) string {
	if auth := strings.TrimSpace(r.Header.Get("Authorization")); auth != "" {
		const prefix = "Bearer "
		if strings.HasPrefix(auth, prefix) {
			return strings.TrimSpace(auth[len(prefix):])
		}
		if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
			return strings.TrimSpace(auth[7:])
		}
	}
	if k := strings.TrimSpace(r.Header.Get("X-API-Key")); k != "" {
		return k
	}
	return strings.TrimSpace(r.URL.Query().Get("key"))
}

func (s *Server) helperUserFromKey(r *http.Request) (userID int64, account string, err error) {
	if s.DB == nil {
		return 0, "", httpx.BadRequest("WALLET_UNAVAILABLE", "wallet unavailable")
	}
	key := extractHelperAPIKey(r)
	if key == "" || len(key) > 256 {
		return 0, "", httpx.Unauthorized("KEY_REQUIRED", "api key required")
	}
	sum := sha256.Sum256([]byte(key))
	err = s.DB.QueryRow(r.Context(), `
		SELECT k.user_id, COALESCE(NULLIF(TRIM(u.email), ''), '')
		FROM buyer_keys k
		JOIN users u ON u.id = k.user_id
		WHERE k.key_hash = $1 AND k.status = 'active' AND k.deleted_at IS NULL
		LIMIT 1`, sum[:]).Scan(&userID, &account)
	if err != nil {
		return 0, "", httpx.Unauthorized("KEY_INVALID", "api key invalid")
	}
	return userID, account, nil
}

// GET /api/one-click/helper/vendors?client=opencode — 该 Agent 可用的保障池厂商（助手下拉）
func (s *Server) helperVendors(w http.ResponseWriter, r *http.Request) error {
	if s.Keys == nil || s.DB == nil {
		return httpx.BadRequest("ONECLICK_UNAVAILABLE", "one-click helper is not configured")
	}
	if _, _, err := s.helperUserFromKey(r); err != nil {
		return err
	}
	client := strings.TrimSpace(r.URL.Query().Get("client"))
	pools, err := scheduling.ListPools(r.Context(), s.DB, true)
	if err != nil {
		return err
	}
	vendors, err := buyer.ListOneClickVendors(client, pools)
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	return ok(w, map[string]any{"client": client, "vendors": vendors})
}

type helperResolveKeyBody struct {
	Client   string `json:"client"`
	Platform string `json:"platform"`
}

// POST /api/one-click/helper/resolve-key — 按厂商复用/创建保障池密钥，返回明文供助手写入客户端
func (s *Server) helperResolveKey(w http.ResponseWriter, r *http.Request) error {
	if s.Keys == nil || s.OneClick == nil {
		return httpx.BadRequest("ONECLICK_UNAVAILABLE", "one-click helper is not configured")
	}
	userID, account, err := s.helperUserFromKey(r)
	if err != nil {
		return err
	}
	var in helperResolveKeyBody
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	platform, err := buyer.ValidateAgentPlatform(in.Client, in.Platform)
	if err != nil {
		return err
	}
	client := strings.TrimSpace(strings.ToLower(in.Client))
	k, err := s.Keys.ResolveOrCreateOneClickKey(r.Context(), userID, client, platform)
	if err != nil {
		return err
	}
	plain, err := s.Keys.Secret(r.Context(), userID, k.ID)
	if err != nil {
		return err
	}
	detail, err := s.Keys.Get(r.Context(), userID, k.ID)
	if err != nil {
		return err
	}
	baseURL := strings.TrimRight(strings.TrimSpace(s.OneClick.EdgeURL), "/")
	if baseURL == "" {
		baseURL = requestAPIBase(r, "")
	}
	w.Header().Set("Cache-Control", "no-store")
	return ok(w, map[string]any{
		"key":      plain,
		"key_id":   k.ID,
		"key_name": detail.Name,
		"platform": detail.Platform,
		"base_url": baseURL,
		"provider": "闲猫",
		"client":   client,
		"user_id":  userID,
		"account":  account,
	})
}

// GET /api/one-click/helper/wallet — 助手用换票得到的买家 Key 查余额（无需网站 JWT）
func (s *Server) helperWallet(w http.ResponseWriter, r *http.Request) error {
	if s.Funds == nil {
		return httpx.BadRequest("WALLET_UNAVAILABLE", "wallet unavailable")
	}
	userID, account, err := s.helperUserFromKey(r)
	if err != nil {
		return err
	}
	wl, err := s.Funds.Wallet(r.Context(), userID)
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	return ok(w, map[string]any{
		"ok":           true,
		"user_id":      userID,
		"account":      account,
		"balance":      wl.Balance,
		"refundable":   wl.Refundable,
		"ledger_cash":  wl.LedgerCash,
		"recharge_url": "/buyer/wallet",
	})
}

// GET /api/one-click/helper/recharge/options — 助手内充值：可用渠道与金额上下限
func (s *Server) helperRechargeOptions(w http.ResponseWriter, r *http.Request) error {
	if s.Funds == nil {
		return httpx.BadRequest("WALLET_UNAVAILABLE", "wallet unavailable")
	}
	if _, _, err := s.helperUserFromKey(r); err != nil {
		return err
	}
	channels, err := s.Funds.RechargeChannels(r.Context())
	if err != nil {
		return err
	}
	cfg, err := s.Funds.RechargeConfig(r.Context())
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	return ok(w, map[string]any{
		"channels":     channels,
		"recharge_min": cfg.MinAmount,
		"recharge_max": cfg.MaxAmount,
	})
}

type helperRechargeBody struct {
	Amount  float64 `json:"amount"`
	Channel string  `json:"channel"`
	Origin  string  `json:"origin"` // 可选：网站根地址，用于支付宝网页支付回跳
}

// POST /api/one-click/helper/recharge — 助手用买家 Key 创建充值单（扫码可在助手内完成）
func (s *Server) helperCreateRecharge(w http.ResponseWriter, r *http.Request) error {
	if s.Funds == nil {
		return httpx.BadRequest("WALLET_UNAVAILABLE", "wallet unavailable")
	}
	userID, _, err := s.helperUserFromKey(r)
	if err != nil {
		return err
	}
	var in helperRechargeBody
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	origin := strings.TrimSpace(in.Origin)
	if origin == "" {
		origin = requestOrigin(r, s.TrustedProxies)
	}
	o, err := s.Funds.CreateRecharge(r.Context(), userID, in.Amount, in.Channel, httpx.ClientIP(r), origin)
	if err != nil {
		return err
	}
	if s.Accounts != nil {
		s.Accounts.RecordIP(r.Context(), userID, httpx.ClientIP(r), "recharge")
	}
	w.Header().Set("Cache-Control", "no-store")
	return created(w, map[string]any{"order": o, "payment": o.Payment})
}

// GET /api/one-click/helper/recharge/{id} — 助手轮询充值单（会触发查单入账）
func (s *Server) helperGetRecharge(w http.ResponseWriter, r *http.Request) error {
	if s.Funds == nil {
		return httpx.BadRequest("WALLET_UNAVAILABLE", "wallet unavailable")
	}
	userID, _, err := s.helperUserFromKey(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	o, err := s.Funds.RefreshRecharge(r.Context(), userID, id)
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	return ok(w, o)
}
