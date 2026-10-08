// Package merchants 管理站内配置的支付商户（支付宝应用、微信支付商户号）：保存配置与加密的密钥，
// 并按当前配置动态构建支付渠道客户端。配置修改后约 5 秒内在各进程生效，无需重启。
package merchants

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/DueGin/token-market/internal/payment"
	"github.com/DueGin/token-market/internal/payment/alipay"
	"github.com/DueGin/token-market/internal/payment/wechat"
	"github.com/DueGin/token-market/internal/platform/crypto"
	"github.com/DueGin/token-market/internal/platform/httpx"
	"github.com/DueGin/token-market/internal/platform/randx"
	"github.com/DueGin/token-market/internal/settings"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type DB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// AlipayConfig 是支付宝应用的非敏感配置。公钥模式填写 AlipayPublicKey；公钥证书模式填写三个证书。
type AlipayConfig struct {
	AppID           string `json:"app_id"`
	Gateway         string `json:"gateway"` // 为空时使用正式环境
	Mode            string `json:"mode"`    // public_key / cert
	AlipayPublicKey string `json:"alipay_public_key"`
	AppCert         string `json:"app_cert"`         // 应用公钥证书 appCertPublicKey_xxx.crt
	AlipayCert      string `json:"alipay_cert"`      // 支付宝公钥证书 alipayCertPublicKey_RSA2.crt
	AlipayRootCert  string `json:"alipay_root_cert"` // 支付宝根证书 alipayRootCert.crt
}

type alipaySecrets struct {
	PrivateKey string `json:"private_key"` // 应用私钥
}

// WechatConfig 是微信支付商户的非敏感配置。填写微信支付公钥时用公钥验签，否则自动下载平台证书。
type WechatConfig struct {
	AppID       string `json:"app_id"`
	MchID       string `json:"mch_id"`
	MchSerialNo string `json:"mch_serial_no"` // 商户 API 证书序列号
	PublicKeyID string `json:"public_key_id"` // 微信支付公钥 ID（PUB_KEY_ID_ 开头）
	PublicKey   string `json:"public_key"`
	BaseURL     string `json:"base_url"` // 为空时使用 https://api.mch.weixin.qq.com
}

type wechatSecrets struct {
	PrivateKey string `json:"private_key"` // 商户 API 私钥 apiclient_key.pem
	APIv3Key   string `json:"api_v3_key"`
}

// View 是后台看到的商户配置：密钥只显示是否已设置。
type View struct {
	Provider   string        `json:"provider"`
	Enabled    bool          `json:"enabled"`
	Alipay     *AlipayConfig `json:"alipay,omitempty"`
	Wechat     *WechatConfig `json:"wechat,omitempty"`
	HasPrivate bool          `json:"has_private_key"`
	HasAPIv3   bool          `json:"has_api_v3_key,omitempty"`
	Ready      bool          `json:"ready"`             // 配置完整且能构建客户端
	Problem    string        `json:"problem,omitempty"` // 不能使用的原因
	UpdatedAt  *time.Time    `json:"updated_at,omitempty"`
}

// Input 是保存商户配置的请求。密钥字段为空表示保持不变。
type Input struct {
	Enabled    *bool         `json:"enabled"`
	Alipay     *AlipayConfig `json:"alipay"`
	Wechat     *WechatConfig `json:"wechat"`
	PrivateKey string        `json:"private_key"`
	APIv3Key   string        `json:"api_v3_key"`
}

type row struct {
	provider  string
	enabled   bool
	config    []byte
	secrets   []byte
	updatedAt time.Time
}

func aad(provider string) string { return "payment-merchant:" + provider }

func load(ctx context.Context, db DB, provider string) (*row, error) {
	var r row
	err := db.QueryRow(ctx, `SELECT provider, enabled, config, secrets_enc, updated_at FROM payment_merchants WHERE provider = $1`, provider).
		Scan(&r.provider, &r.enabled, &r.config, &r.secrets, &r.updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &r, err
}

func validProvider(p string) bool { return p == payment.ProviderAlipay || p == payment.ProviderWechat }

// placeholderNotify 用于校验配置时构建客户端（尚未填写通知地址时）。
const placeholderNotify = "https://notify.invalid"

// Store 读写商户配置，并缓存按配置构建的渠道客户端。
type Store struct {
	DB         DB
	Keyring    *crypto.Keyring
	Settings   func(ctx context.Context) (settings.Settings, error)
	Log        *slog.Logger
	HTTPClient *http.Client // 访问渠道接口，测试可替换

	mu       sync.Mutex
	reg      *payment.Registry
	problems map[string]string
	fp       string
	checked  time.Time
}

// Registry 返回按当前配置构建的渠道。配置未变时复用已有客户端（微信平台证书等缓存得以保留），最多每 5 秒检查一次变化。
func (s *Store) Registry(ctx context.Context) (*payment.Registry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reg != nil && time.Since(s.checked) < 5*time.Second {
		return s.reg, nil
	}
	st, err := s.Settings(ctx)
	if err != nil {
		return nil, err
	}
	var fp string
	if err := s.DB.QueryRow(ctx, `SELECT COALESCE(string_agg(provider || ':' || enabled || ':' || updated_at::text, ',' ORDER BY provider), '')
		FROM payment_merchants`).Scan(&fp); err != nil {
		return nil, err
	}
	fp += "|" + st.PaymentPublicURL
	if s.reg != nil && fp == s.fp {
		s.checked = time.Now()
		return s.reg, nil
	}
	var providers []payment.Provider
	var disabled []string
	problems := map[string]string{}
	for _, name := range []string{payment.ProviderAlipay, payment.ProviderWechat} {
		r, err := load(ctx, s.DB, name)
		if err != nil {
			return nil, err
		}
		if r == nil {
			continue
		}
		p, err := s.build(r, st.PaymentPublicURL)
		if err != nil {
			problems[name] = err.Error()
			continue
		}
		providers = append(providers, p)
		if !r.enabled {
			disabled = append(disabled, name)
		}
	}
	reg := payment.NewRegistry(providers...)
	for _, d := range disabled {
		reg.Disable(d)
	}
	s.reg, s.problems, s.fp, s.checked = reg, problems, fp, time.Now()
	return reg, nil
}

// Invalidate 让下一次 Registry 立即重新检查配置（本进程内修改配置后调用）。
func (s *Store) Invalidate() {
	s.mu.Lock()
	s.checked = time.Time{}
	s.mu.Unlock()
}

func notifyURL(publicURL, provider string) (string, error) {
	if publicURL == "" {
		return "", errors.New("请先填写支付通知地址（支付配置 → 基础设置）")
	}
	return publicURL + "/api/payments/" + provider + "/notify", nil
}

func (s *Store) build(r *row, publicURL string) (payment.Provider, error) {
	var sec []byte
	if len(r.secrets) > 0 {
		plain, err := s.Keyring.OpenString(r.secrets, aad(r.provider))
		if err != nil {
			return nil, fmt.Errorf("解密商户密钥失败：%w", err)
		}
		sec = []byte(plain)
	}
	notify, err := notifyURL(publicURL, r.provider)
	if err != nil {
		return nil, err
	}
	return s.buildWith(r.provider, r.config, sec, notify)
}

func (s *Store) buildWith(provider string, config, secrets []byte, notify string) (payment.Provider, error) {
	switch provider {
	case payment.ProviderAlipay:
		var c AlipayConfig
		var sec alipaySecrets
		if err := json.Unmarshal(config, &c); err != nil {
			return nil, err
		}
		if len(secrets) > 0 {
			if err := json.Unmarshal(secrets, &sec); err != nil {
				return nil, err
			}
		}
		return buildAlipay(c, sec, notify, s.HTTPClient)
	case payment.ProviderWechat:
		var c WechatConfig
		var sec wechatSecrets
		if err := json.Unmarshal(config, &c); err != nil {
			return nil, err
		}
		if len(secrets) > 0 {
			if err := json.Unmarshal(secrets, &sec); err != nil {
				return nil, err
			}
		}
		return buildWechat(c, sec, notify, s.HTTPClient)
	}
	return nil, fmt.Errorf("unknown provider %s", provider)
}

func buildAlipay(c AlipayConfig, sec alipaySecrets, notify string, client *http.Client) (*alipay.Client, error) {
	if c.AppID == "" {
		return nil, errors.New("请填写支付宝应用 APPID")
	}
	if sec.PrivateKey == "" {
		return nil, errors.New("请填写应用私钥")
	}
	priv, err := payment.ParsePrivateKey(sec.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("应用私钥无效：%w", err)
	}
	cfg := alipay.Config{AppID: c.AppID, Gateway: c.Gateway, PrivateKey: priv, NotifyURL: notify, HTTPClient: client,
		Channels: []string{payment.ChannelAlipayPage, payment.ChannelAlipayF2F}}
	if c.Mode == "cert" {
		if c.AppCert == "" || c.AlipayCert == "" || c.AlipayRootCert == "" {
			return nil, errors.New("公钥证书模式需要应用公钥证书、支付宝公钥证书与支付宝根证书")
		}
		appCert, err := payment.ParseCertificate(c.AppCert)
		if err != nil {
			return nil, fmt.Errorf("应用公钥证书无效：%w", err)
		}
		alipayCert, err := payment.ParseCertificate(c.AlipayCert)
		if err != nil {
			return nil, fmt.Errorf("支付宝公钥证书无效：%w", err)
		}
		pk, ok := alipayCert.PublicKey.(*rsa.PublicKey)
		if !ok {
			return nil, errors.New("支付宝公钥证书不是 RSA 证书")
		}
		if cfg.AlipayRootCertSN, err = alipay.RootCertSN(c.AlipayRootCert); err != nil {
			return nil, fmt.Errorf("支付宝根证书无效：%w", err)
		}
		cfg.AppCertSN, cfg.AlipayPublicKey = alipay.CertSN(appCert), pk
	} else {
		if c.AlipayPublicKey == "" {
			return nil, errors.New("请填写支付宝公钥")
		}
		if cfg.AlipayPublicKey, err = payment.ParsePublicKey(c.AlipayPublicKey); err != nil {
			return nil, fmt.Errorf("支付宝公钥无效：%w", err)
		}
	}
	return alipay.New(cfg)
}

func buildWechat(c WechatConfig, sec wechatSecrets, notify string, client *http.Client) (*wechat.Client, error) {
	switch {
	case c.AppID == "" || c.MchID == "":
		return nil, errors.New("请填写 AppID 与商户号")
	case c.MchSerialNo == "":
		return nil, errors.New("请填写商户 API 证书序列号")
	case sec.PrivateKey == "":
		return nil, errors.New("请填写商户 API 私钥")
	case len(sec.APIv3Key) != 32:
		return nil, errors.New("APIv3 密钥必须是 32 个字符")
	case (c.PublicKeyID == "") != (c.PublicKey == ""):
		return nil, errors.New("微信支付公钥 ID 与公钥需要同时填写")
	}
	priv, err := payment.ParsePrivateKey(sec.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("商户 API 私钥无效：%w", err)
	}
	cfg := wechat.Config{AppID: c.AppID, MchID: c.MchID, MchSerialNo: c.MchSerialNo, PrivateKey: priv, APIv3Key: sec.APIv3Key,
		BaseURL: c.BaseURL, NotifyURL: notify, HTTPClient: client, PublicKeyID: c.PublicKeyID}
	if c.PublicKey != "" {
		if cfg.PublicKey, err = payment.ParsePublicKey(c.PublicKey); err != nil {
			return nil, fmt.Errorf("微信支付公钥无效：%w", err)
		}
	}
	return wechat.New(cfg)
}

// Get 返回商户配置（密钥不返回）。
func (s *Store) Get(ctx context.Context, provider string) (*View, error) {
	if !validProvider(provider) {
		return nil, httpx.NotFound("PROVIDER_NOT_FOUND", "unknown payment provider")
	}
	r, err := load(ctx, s.DB, provider)
	if err != nil {
		return nil, err
	}
	v := &View{Provider: provider}
	if provider == payment.ProviderAlipay {
		v.Alipay = &AlipayConfig{Mode: "public_key"}
	} else {
		v.Wechat = &WechatConfig{}
	}
	if r == nil {
		v.Problem = "尚未配置"
		return v, nil
	}
	v.Enabled, v.UpdatedAt = r.enabled, &r.updatedAt
	if v.Alipay != nil {
		_ = json.Unmarshal(r.config, v.Alipay)
	} else {
		_ = json.Unmarshal(r.config, v.Wechat)
	}
	if len(r.secrets) > 0 {
		if plain, err := s.Keyring.OpenString(r.secrets, aad(provider)); err == nil {
			var sec wechatSecrets // 两家的私钥字段同名
			_ = json.Unmarshal([]byte(plain), &sec)
			v.HasPrivate, v.HasAPIv3 = sec.PrivateKey != "", sec.APIv3Key != ""
		}
	}
	if _, err := s.Registry(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	v.Problem = s.problems[provider]
	built := s.reg.Get(provider) != nil
	s.mu.Unlock()
	v.Ready = built && v.Problem == ""
	return v, nil
}

// Update 保存商户配置。保存前用新配置构建一次客户端，密钥或证书无效时拒绝保存。
func (s *Store) Update(ctx context.Context, provider string, in Input, adminID int64) (*View, error) {
	if !validProvider(provider) {
		return nil, httpx.NotFound("PROVIDER_NOT_FOUND", "unknown payment provider")
	}
	r, err := load(ctx, s.DB, provider)
	if err != nil {
		return nil, err
	}
	if r == nil {
		r = &row{provider: provider, config: []byte("{}")}
	}
	// 合并密钥：空值表示保持不变
	secrets := map[string]string{}
	if len(r.secrets) > 0 {
		plain, err := s.Keyring.OpenString(r.secrets, aad(provider))
		if err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(plain), &secrets)
	}
	if v := strings.TrimSpace(in.PrivateKey); v != "" {
		secrets["private_key"] = v
	}
	if v := strings.TrimSpace(in.APIv3Key); v != "" {
		if provider != payment.ProviderWechat {
			return nil, httpx.BadRequest("INVALID_FIELD", "api_v3_key is only for wechat pay")
		}
		secrets["api_v3_key"] = v
	}
	config := r.config
	switch provider {
	case payment.ProviderAlipay:
		if in.Alipay != nil {
			c := *in.Alipay
			c.AppID, c.Gateway = strings.TrimSpace(c.AppID), strings.TrimRight(strings.TrimSpace(c.Gateway), "/")
			if c.Mode != "cert" {
				c.Mode, c.AppCert, c.AlipayCert, c.AlipayRootCert = "public_key", "", "", ""
			} else {
				c.AlipayPublicKey = ""
			}
			if c.Gateway != "" {
				if err := checkURL(c.Gateway); err != nil {
					return nil, httpx.BadRequest("INVALID_GATEWAY", "支付宝网关地址无效")
				}
			}
			config, _ = json.Marshal(c)
		}
	case payment.ProviderWechat:
		if in.Wechat != nil {
			c := *in.Wechat
			c.AppID, c.MchID, c.MchSerialNo = strings.TrimSpace(c.AppID), strings.TrimSpace(c.MchID), strings.TrimSpace(c.MchSerialNo)
			c.PublicKeyID, c.BaseURL = strings.TrimSpace(c.PublicKeyID), strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
			if c.BaseURL != "" {
				if err := checkURL(c.BaseURL); err != nil {
					return nil, httpx.BadRequest("INVALID_BASE_URL", "微信支付接口地址无效")
				}
			}
			config, _ = json.Marshal(c)
		}
	}
	enabled := r.enabled
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	secretJSON, _ := json.Marshal(secrets)
	// 启用时必须能构建客户端；未启用时允许保存不完整的配置（逐步填写）
	if _, err := s.buildWith(provider, config, secretJSON, placeholderNotify); err != nil && enabled {
		return nil, httpx.BadRequest("INVALID_MERCHANT", err.Error())
	}
	enc, err := s.Keyring.SealString(string(secretJSON), aad(provider))
	if err != nil {
		return nil, err
	}
	if _, err := s.DB.Exec(ctx, `INSERT INTO payment_merchants (provider, enabled, config, secrets_enc, updated_by)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (provider) DO UPDATE SET enabled = EXCLUDED.enabled, config = EXCLUDED.config, secrets_enc = EXCLUDED.secrets_enc,
			updated_by = EXCLUDED.updated_by, updated_at = now()`, provider, enabled, config, enc, adminID); err != nil {
		return nil, err
	}
	s.Invalidate()
	return s.Get(ctx, provider)
}

func checkURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return errors.New("invalid url")
	}
	return nil
}

// Test 用当前配置向渠道查询一个不存在的订单：渠道返回"订单不存在"说明签名、密钥与验签都正确。
func (s *Store) Test(ctx context.Context, provider string) error {
	reg, err := s.Registry(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	problem := s.problems[provider]
	s.mu.Unlock()
	if problem != "" {
		return httpx.BadRequest("MERCHANT_NOT_READY", problem)
	}
	p := reg.Get(provider)
	if p == nil {
		return httpx.BadRequest("MERCHANT_NOT_READY", "尚未配置")
	}
	tctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	_, err = p.Query(tctx, "TMPING"+randx.String(16))
	switch {
	case errors.Is(err, payment.ErrTradeNotFound):
		return nil
	case err == nil:
		return nil
	}
	return httpx.BadRequest("MERCHANT_TEST_FAILED", err.Error())
}
