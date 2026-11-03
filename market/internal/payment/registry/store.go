// Package registry adapts hostpay snapshots to market payment clients.
package registry

import (
	"context"
	"crypto/rsa"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/yx-token/token-market/market/hostpay"
	"github.com/yx-token/token-market/market/internal/payment"
	"github.com/yx-token/token-market/market/internal/payment/alipay"
	"github.com/yx-token/token-market/market/internal/payment/wechat"
)

const cacheTTL = 5 * time.Second

// Store caches host configuration and clients. Credentials are never persisted by market.
type Store struct {
	Load          hostpay.Source
	AlipayGateway string
	WechatBaseURL string
	Log           *slog.Logger
	HTTPClient    *http.Client

	mu      sync.Mutex
	reg     *payment.Registry
	cfg     *hostpay.Config
	clients map[int64]cachedClient
	checked time.Time
}

type cachedClient struct {
	fingerprint string
	provider    payment.Provider
}

// Registry returns the current instance-aware payment registry.
func (s *Store) Registry(ctx context.Context) (*payment.Registry, error) {
	reg, _, err := s.load(ctx)
	return reg, err
}

// Config returns host payment limits normalized to market defaults and bounds.
func (s *Store) Config(ctx context.Context) (*hostpay.Config, error) {
	_, cfg, err := s.load(ctx)
	return cfg, err
}

// Invalidate forces the next call to reload the host snapshot.
func (s *Store) Invalidate() {
	s.mu.Lock()
	s.checked = time.Time{}
	s.mu.Unlock()
}

func (s *Store) load(ctx context.Context) (*payment.Registry, *hostpay.Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reg != nil && time.Since(s.checked) < cacheTTL {
		return s.reg, cloneConfig(s.cfg), nil
	}
	loader := s.Load
	if loader == nil {
		loader = hostpay.Load
	}
	snapshot, err := loader(ctx)
	if err != nil {
		if errors.Is(err, hostpay.ErrNoSource) {
			cfg := &hostpay.Config{MinAmount: 1, MaxAmount: 50000, OrderTimeoutMinutes: 30}
			reg := payment.NewInstanceRegistry()
			s.reg, s.cfg, s.clients, s.checked = reg, cfg, nil, time.Now()
			return reg, cloneConfig(cfg), nil
		}
		return nil, nil, err
	}
	if snapshot == nil {
		snapshot = &hostpay.Config{}
	}
	cfg := normalizeConfig(snapshot)
	// Callers only need global limits; avoid retaining decrypted credentials in the config cache.
	cfg.Instances = nil

	instances := make([]payment.Instance, 0, len(snapshot.Instances))
	clients := make(map[int64]cachedClient, len(snapshot.Instances))
	for _, inst := range snapshot.Instances {
		if inst.ID <= 0 || (inst.ProviderKey != hostpay.ProviderAlipay && inst.ProviderKey != hostpay.ProviderWxpay) {
			continue
		}
		origin, err := notifyOrigin(inst.Config["notifyUrl"])
		if err != nil {
			s.problem(inst, "invalid notifyUrl: %v", err)
			continue
		}
		instanceFP := instanceFingerprint(inst, s.AlipayGateway, s.WechatBaseURL)
		client, ok := s.clients[inst.ID]
		if !ok || client.fingerprint != instanceFP {
			var p payment.Provider
			supported := supportedMethod(inst.SupportedTypes, inst.ProviderKey)
			switch inst.ProviderKey {
			case hostpay.ProviderAlipay:
				p, err = s.buildAlipay(inst, origin, supported)
			case hostpay.ProviderWxpay:
				p, err = s.buildWechat(inst, origin, supported)
			}
			if err != nil {
				s.problem(inst, "client configuration invalid: %v", err)
				continue
			}
			client = cachedClient{fingerprint: instanceFP, provider: p}
		}
		clients[inst.ID] = client
		instances = append(instances, payment.Instance{ID: inst.ID, Provider: client.provider, Enabled: inst.Enabled,
			SortOrder: inst.SortOrder, Origin: origin})
	}
	reg := payment.NewInstanceRegistry(instances...)
	reg.SetEnabled(cfg.Enabled)
	s.reg, s.cfg, s.clients, s.checked = reg, cfg, clients, time.Now()
	return reg, cloneConfig(cfg), nil
}

func (s *Store) buildAlipay(inst hostpay.Instance, origin string, methods map[string]bool) (payment.Provider, error) {
	c := inst.Config
	if !methods["alipay"] {
		return nil, errors.New("supported_types does not include alipay")
	}
	if strings.TrimSpace(c["appId"]) == "" {
		return nil, errors.New("appId is required")
	}
	priv, err := payment.ParsePrivateKey(c["privateKey"])
	if err != nil {
		return nil, fmt.Errorf("privateKey: %w", err)
	}
	pubRaw := c["publicKey"]
	if pubRaw == "" {
		pubRaw = c["alipayPublicKey"]
	}
	pub, err := payment.ParsePublicKey(pubRaw)
	if err != nil {
		return nil, fmt.Errorf("publicKey: %w", err)
	}
	channels := make([]string, 0, 2)
	if methods["alipay"] {
		channels = append(channels, payment.ChannelAlipayPage)
		if !strings.EqualFold(strings.TrimSpace(firstNonEmpty(inst.PaymentMode, c["paymentMode"])), "redirect") {
			channels = append(channels, payment.ChannelAlipayF2F)
		}
	}
	return alipay.New(alipay.Config{AppID: c["appId"], Gateway: s.AlipayGateway, PrivateKey: priv,
		AlipayPublicKey: pub, NotifyURL: notifyURL(origin, payment.ProviderAlipay, inst.ID), Channels: channels, HTTPClient: s.HTTPClient})
}

func (s *Store) buildWechat(inst hostpay.Instance, origin string, methods map[string]bool) (payment.Provider, error) {
	if !methods["wxpay"] {
		return nil, errors.New("supported_types does not include wxpay")
	}
	c := inst.Config
	if strings.TrimSpace(c["publicKey"]) == "" || strings.TrimSpace(c["publicKeyId"]) == "" {
		return nil, errors.New("publicKey and publicKeyId are required by the host wxpay provider")
	}
	priv, err := payment.ParsePrivateKey(c["privateKey"])
	if err != nil {
		return nil, fmt.Errorf("privateKey: %w", err)
	}
	var pub *rsa.PublicKey
	if c["publicKey"] != "" || c["publicKeyId"] != "" {
		parsed, err := payment.ParsePublicKey(c["publicKey"])
		if err != nil {
			return nil, fmt.Errorf("publicKey: %w", err)
		}
		pub = parsed
	}
	var publicKeyID string
	if pub != nil {
		publicKeyID = c["publicKeyId"]
	}
	return wechat.New(wechat.Config{AppID: c["appId"], MchID: c["mchId"], MchSerialNo: c["certSerial"],
		PrivateKey: priv, APIv3Key: c["apiV3Key"], PublicKeyID: publicKeyID, PublicKey: pub,
		BaseURL: s.WechatBaseURL, NotifyURL: notifyURL(origin, payment.ProviderWechat, inst.ID), HTTPClient: s.HTTPClient})
}

func supportedMethod(raw, provider string) map[string]bool {
	method := "alipay"
	if provider == hostpay.ProviderWxpay {
		method = "wxpay"
	}
	if strings.TrimSpace(raw) == "" {
		return map[string]bool{method: true}
	}
	for _, item := range strings.Split(raw, ",") {
		t := strings.ToLower(strings.TrimSpace(item))
		if t == method || (provider == hostpay.ProviderAlipay && t == "alipay_direct") || (provider == hostpay.ProviderWxpay && t == "wxpay_direct") {
			return map[string]bool{method: true}
		}
	}
	return map[string]bool{}
}

func notifyOrigin(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return "", errors.New("notifyUrl must be an absolute http(s) URL")
	}
	return u.Scheme + "://" + u.Host, nil
}

func notifyURL(origin, provider string, id int64) string {
	return fmt.Sprintf("%s/api/payments/%s/%d/notify", origin, provider, id)
}

func normalizeConfig(src *hostpay.Config) *hostpay.Config {
	cfg := cloneConfig(src)
	if cfg.MinAmount <= 0 {
		cfg.MinAmount = 1
	}
	if cfg.MaxAmount <= 0 {
		cfg.MaxAmount = 50000
	}
	if cfg.OrderTimeoutMinutes <= 0 {
		cfg.OrderTimeoutMinutes = 30
	}
	if cfg.OrderTimeoutMinutes < 5 {
		cfg.OrderTimeoutMinutes = 5
	}
	if cfg.OrderTimeoutMinutes > 1440 {
		cfg.OrderTimeoutMinutes = 1440
	}
	return cfg
}

func cloneConfig(src *hostpay.Config) *hostpay.Config {
	if src == nil {
		return &hostpay.Config{}
	}
	cfg := *src
	cfg.Instances = nil
	return &cfg
}

func instanceFingerprint(inst hostpay.Instance, alipayGateway, wechatBaseURL string) string {
	fingerprint := fmt.Sprintf("%d:%s", inst.ID, inst.UpdatedAt.UTC().Format(time.RFC3339Nano))
	switch inst.ProviderKey {
	case hostpay.ProviderAlipay:
		fingerprint += "|" + alipayGateway
	case hostpay.ProviderWxpay:
		fingerprint += "|" + wechatBaseURL
	}
	return fingerprint
}

func (s *Store) problem(inst hostpay.Instance, format string, args ...any) {
	if s.Log != nil {
		attrs := []any{"instance_id", inst.ID, "provider", inst.ProviderKey, "name", inst.Name,
			"reason", fmt.Sprintf(format, args...)}
		s.Log.Warn("host payment instance excluded", attrs...)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
