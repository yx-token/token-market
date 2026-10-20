// Package wechat 是微信支付 APIv3 的客户端：Native 下单（扫码支付）、查单、关单、退款与异步通知。
//
// 应答与通知验签支持两种方式：微信支付公钥（public_key_id 以 PUB_KEY_ID_ 开头，新商户默认），
// 或平台证书（未配置公钥时通过 /v3/certificates 自动下载并定期刷新）。
package wechat

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/yx-token/idlicat/market/internal/payment"
	"github.com/yx-token/idlicat/market/internal/platform/randx"
)

const BaseURLProd = "https://api.mch.weixin.qq.com"

type Config struct {
	AppID       string
	MchID       string
	MchSerialNo string          // 商户 API 证书序列号
	PrivateKey  *rsa.PrivateKey // 商户 API 私钥
	APIv3Key    string          // 32 字节 APIv3 密钥，用于解密通知与平台证书
	// 微信支付公钥模式；为空时使用平台证书
	PublicKeyID string
	PublicKey   *rsa.PublicKey
	BaseURL     string
	NotifyURL   string
	HTTPClient  *http.Client
	Now         func() time.Time
}

type Client struct {
	cfg Config

	mu        sync.RWMutex
	certs     map[string]*rsa.PublicKey // 平台证书序列号 → 公钥
	certsAt   time.Time
	refreshMu sync.Mutex
}

func New(cfg Config) (*Client, error) {
	switch {
	case cfg.AppID == "" || cfg.MchID == "":
		return nil, errors.New("wechat: app_id and mch_id are required")
	case cfg.MchSerialNo == "" || cfg.PrivateKey == nil:
		return nil, errors.New("wechat: merchant certificate serial number and private key are required")
	case len(cfg.APIv3Key) != 32:
		return nil, errors.New("wechat: api_v3_key must be 32 bytes")
	case cfg.NotifyURL == "":
		return nil, errors.New("wechat: notify url is required (payment.public_url)")
	case (cfg.PublicKeyID == "") != (cfg.PublicKey == nil):
		return nil, errors.New("wechat: public_key_id and public key must be configured together")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = BaseURLProd
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 15 * time.Second}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Client{cfg: cfg, certs: map[string]*rsa.PublicKey{}}, nil
}

func (c *Client) Name() string       { return payment.ProviderWechat }
func (c *Client) Channels() []string { return []string{payment.ChannelWechatNative} }

// ---- 签名与验签 ----

// Authorization 生成请求的 Authorization 头。
func (c *Client) Authorization(method, canonicalURL, body string) (string, error) {
	ts := strconv.FormatInt(c.cfg.Now().Unix(), 10)
	nonce := randx.String(32)
	msg := method + "\n" + canonicalURL + "\n" + ts + "\n" + nonce + "\n" + body + "\n"
	sig, err := payment.SignSHA256(c.cfg.PrivateKey, msg)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`WECHATPAY2-SHA256-RSA2048 mchid="%s",nonce_str="%s",signature="%s",timestamp="%s",serial_no="%s"`,
		c.cfg.MchID, nonce, sig, ts, c.cfg.MchSerialNo), nil
}

// verify 校验应答或通知的签名：timestamp\nnonce\nbody\n。
func (c *Client) verify(ctx context.Context, h http.Header, body []byte) error {
	serial := h.Get("Wechatpay-Serial")
	sig := h.Get("Wechatpay-Signature")
	ts := h.Get("Wechatpay-Timestamp")
	nonce := h.Get("Wechatpay-Nonce")
	if serial == "" || sig == "" || ts == "" || nonce == "" {
		return errors.New("missing wechatpay signature headers")
	}
	key, err := c.verifyKey(ctx, serial)
	if err != nil {
		return err
	}
	return payment.VerifySHA256(key, ts+"\n"+nonce+"\n"+string(body)+"\n", sig)
}

func (c *Client) verifyKey(ctx context.Context, serial string) (*rsa.PublicKey, error) {
	if c.cfg.PublicKeyID != "" && serial == c.cfg.PublicKeyID {
		return c.cfg.PublicKey, nil
	}
	c.mu.RLock()
	key, fresh := c.certs[serial], time.Since(c.certsAt) < 12*time.Hour
	c.mu.RUnlock()
	if key != nil && fresh {
		return key, nil
	}
	if c.cfg.PublicKeyID != "" && key == nil {
		return nil, fmt.Errorf("unknown wechatpay serial %q (expected public key %s)", serial, c.cfg.PublicKeyID)
	}
	if err := c.refreshCerts(ctx); err != nil {
		if key != nil {
			return key, nil // 刷新失败时继续使用已有证书
		}
		return nil, fmt.Errorf("download platform certificates: %w", err)
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if key = c.certs[serial]; key == nil {
		return nil, fmt.Errorf("unknown wechatpay platform certificate %q", serial)
	}
	return key, nil
}

// refreshCerts 下载并解密平台证书，再用下载到的证书校验这次应答的签名。
func (c *Client) refreshCerts(ctx context.Context) error {
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()
	status, h, body, err := c.send(ctx, http.MethodGet, "/v3/certificates", nil)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return apiError(status, body)
	}
	var res struct {
		Data []struct {
			SerialNo           string   `json:"serial_no"`
			EncryptCertificate resource `json:"encrypt_certificate"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return err
	}
	certs := map[string]*rsa.PublicKey{}
	for _, d := range res.Data {
		plain, err := c.decrypt(d.EncryptCertificate)
		if err != nil {
			return err
		}
		block, _ := pem.Decode(plain)
		if block == nil {
			return errors.New("platform certificate: invalid PEM")
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return err
		}
		pk, ok := cert.PublicKey.(*rsa.PublicKey)
		if !ok {
			return errors.New("platform certificate: not RSA")
		}
		certs[d.SerialNo] = pk
	}
	key := certs[h.Get("Wechatpay-Serial")]
	if key == nil {
		return errors.New("platform certificate response signed by unknown certificate")
	}
	if err := payment.VerifySHA256(key, h.Get("Wechatpay-Timestamp")+"\n"+h.Get("Wechatpay-Nonce")+"\n"+string(body)+"\n",
		h.Get("Wechatpay-Signature")); err != nil {
		return fmt.Errorf("platform certificate response signature invalid: %w", err)
	}
	c.mu.Lock()
	c.certs, c.certsAt = certs, time.Now()
	c.mu.Unlock()
	return nil
}

// resource 是 AEAD_AES_256_GCM 加密的数据（通知内容、平台证书）。
type resource struct {
	Algorithm      string `json:"algorithm"`
	Ciphertext     string `json:"ciphertext"`
	AssociatedData string `json:"associated_data"`
	Nonce          string `json:"nonce"`
	OriginalType   string `json:"original_type,omitempty"`
}

// decrypt 用 APIv3 密钥解密。
func (c *Client) decrypt(r resource) ([]byte, error) {
	if r.Algorithm != "AEAD_AES_256_GCM" {
		return nil, fmt.Errorf("unsupported algorithm %q", r.Algorithm)
	}
	ct, err := base64.StdEncoding.DecodeString(r.Ciphertext)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher([]byte(c.cfg.APIv3Key))
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCMWithNonceSize(block, len(r.Nonce))
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, []byte(r.Nonce), ct, []byte(r.AssociatedData))
}

// ---- 请求 ----

func (c *Client) send(ctx context.Context, method, path string, payload any) (int, http.Header, []byte, error) {
	var body []byte
	if payload != nil {
		var err error
		if body, err = json.Marshal(payload); err != nil {
			return 0, nil, nil, err
		}
	}
	auth, err := c.Authorization(method, path, string(body))
	if err != nil {
		return 0, nil, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, c.cfg.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, nil, err
	}
	req.Header.Set("Authorization", auth)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "token-market/1.0")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.cfg.HTTPClient.Do(req)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("wechatpay %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, nil, nil, err
	}
	return resp.StatusCode, resp.Header, b, nil
}

// do 发送请求；2xx 应答必须验签通过，非 2xx 返回 *payment.Error。
func (c *Client) do(ctx context.Context, method, path string, payload, out any) error {
	status, h, body, err := c.send(ctx, method, path, payload)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return apiError(status, body)
	}
	if err := c.verify(ctx, h, body); err != nil {
		return fmt.Errorf("wechatpay %s %s: response signature: %w", method, path, err)
	}
	if out != nil && len(body) > 0 {
		return json.Unmarshal(body, out)
	}
	return nil
}

func apiError(status int, body []byte) error {
	var e struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &e)
	if e.Code == "" {
		e.Code = "HTTP_" + strconv.Itoa(status)
	}
	return &payment.Error{Provider: payment.ProviderWechat, Code: e.Code, Message: e.Message}
}

func errCode(err error) string {
	var pe *payment.Error
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

// ---- 下单、查单、关单 ----

func (c *Client) Create(ctx context.Context, channel string, o payment.Order) (*payment.Intent, error) {
	if channel != payment.ChannelWechatNative {
		return nil, fmt.Errorf("wechat: unsupported channel %q", channel)
	}
	req := map[string]any{
		"appid":        c.cfg.AppID,
		"mchid":        c.cfg.MchID,
		"description":  o.Subject,
		"out_trade_no": o.OutTradeNo,
		"time_expire":  o.ExpireAt.In(payment.China).Format(time.RFC3339),
		"notify_url":   c.cfg.NotifyURL,
		"amount":       map[string]any{"total": o.AmountFen, "currency": "CNY"},
	}
	if o.ClientIP != "" {
		req["scene_info"] = map[string]any{"payer_client_ip": o.ClientIP}
	}
	var res struct {
		CodeURL string `json:"code_url"`
	}
	if err := c.do(ctx, http.MethodPost, "/v3/pay/transactions/native", req, &res); err != nil {
		return nil, err
	}
	if res.CodeURL == "" {
		return nil, errors.New("wechatpay native: missing code_url")
	}
	return &payment.Intent{Kind: "qrcode", QRCode: res.CodeURL}, nil
}

// Transaction 是查单应答与支付通知解密后的交易。
type Transaction struct {
	OutTradeNo    string `json:"out_trade_no"`
	TransactionID string `json:"transaction_id"`
	TradeState    string `json:"trade_state"`
	SuccessTime   string `json:"success_time"`
	Mchid         string `json:"mchid"`
	Appid         string `json:"appid"`
	Amount        struct {
		Total      int64  `json:"total"`
		PayerTotal int64  `json:"payer_total"`
		Currency   string `json:"currency"`
	} `json:"amount"`
}

func (t Transaction) trade() *payment.Trade {
	out := &payment.Trade{OutTradeNo: t.OutTradeNo, TradeNo: t.TransactionID, AmountFen: t.Amount.Total}
	switch t.TradeState {
	case "SUCCESS", "REFUND":
		out.State = payment.TradePaid
	case "CLOSED", "REVOKED", "PAYERROR":
		out.State = payment.TradeClosed
	default: // NOTPAY、USERPAYING
		out.State = payment.TradePending
	}
	if ts, err := time.Parse(time.RFC3339, t.SuccessTime); err == nil {
		out.PaidAt = &ts
	}
	return out
}

func (c *Client) Query(ctx context.Context, outTradeNo string) (*payment.Trade, error) {
	var t Transaction
	path := "/v3/pay/transactions/out-trade-no/" + url.PathEscape(outTradeNo) + "?mchid=" + url.QueryEscape(c.cfg.MchID)
	if err := c.do(ctx, http.MethodGet, path, nil, &t); err != nil {
		if errCode(err) == "ORDER_NOT_EXIST" {
			return nil, payment.ErrTradeNotFound
		}
		return nil, err
	}
	return t.trade(), nil
}

func (c *Client) Close(ctx context.Context, outTradeNo string) error {
	err := c.do(ctx, http.MethodPost, "/v3/pay/transactions/out-trade-no/"+url.PathEscape(outTradeNo)+"/close",
		map[string]string{"mchid": c.cfg.MchID}, nil)
	if errCode(err) == "ORDER_NOT_EXIST" {
		return nil
	}
	return err
}

// ---- 退款 ----

type refundResp struct {
	RefundID string `json:"refund_id"`
	Status   string `json:"status"` // SUCCESS / CLOSED / PROCESSING / ABNORMAL
}

func (r refundResp) result() *payment.RefundResult {
	out := &payment.RefundResult{RefundNo: r.RefundID}
	switch r.Status {
	case "SUCCESS":
		out.State = payment.RefundSuccess
	case "CLOSED", "ABNORMAL":
		out.State = payment.RefundFailed
		out.Message = "refund " + r.Status
	default:
		out.State = payment.RefundProcessing
	}
	return out
}

func (c *Client) Refund(ctx context.Context, r payment.Refund) (*payment.RefundResult, error) {
	req := map[string]any{
		"out_trade_no":  r.OutTradeNo,
		"out_refund_no": r.OutRefundNo,
		"reason":        r.Reason,
		"amount":        map[string]any{"refund": r.AmountFen, "total": r.TotalFen, "currency": "CNY"},
	}
	var res refundResp
	if err := c.do(ctx, http.MethodPost, "/v3/refund/domestic/refunds", req, &res); err != nil {
		var pe *payment.Error
		// 系统繁忙与频率限制可以原样重试，其余业务错误（余额不足、超过退款期限等）需要人工处理
		if errors.As(err, &pe) && pe.Code != "SYSTEM_ERROR" && pe.Code != "FREQUENCY_LIMITED" && pe.Code != "HTTP_500" {
			return &payment.RefundResult{State: payment.RefundFailed, Message: pe.Error()}, nil
		}
		return nil, err
	}
	return res.result(), nil
}

func (c *Client) QueryRefund(ctx context.Context, _, outRefundNo string) (*payment.RefundResult, error) {
	var res refundResp
	if err := c.do(ctx, http.MethodGet, "/v3/refund/domestic/refunds/"+url.PathEscape(outRefundNo), nil, &res); err != nil {
		if errCode(err) == "RESOURCE_NOT_EXISTS" {
			return nil, payment.ErrRefundNotFound
		}
		return nil, err
	}
	return res.result(), nil
}

// ---- 异步通知 ----

type notifyBody struct {
	ID           string   `json:"id"`
	EventType    string   `json:"event_type"`
	ResourceType string   `json:"resource_type"`
	Resource     resource `json:"resource"`
}

func (c *Client) ParseNotify(r *http.Request) (*payment.Trade, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if err := c.verify(r.Context(), r.Header, body); err != nil {
		return nil, fmt.Errorf("wechatpay notify: %w", err)
	}
	// 防重放：签名时间与当前时间相差不超过 5 分钟
	ts, err := strconv.ParseInt(r.Header.Get("Wechatpay-Timestamp"), 10, 64)
	if err != nil || abs(c.cfg.Now().Unix()-ts) > 300 {
		return nil, errors.New("wechatpay notify: timestamp out of range")
	}
	var n notifyBody
	if err := json.Unmarshal(body, &n); err != nil {
		return nil, err
	}
	if n.EventType != "TRANSACTION.SUCCESS" {
		return nil, nil // 退款等其他通知：状态以主动查询为准
	}
	plain, err := c.decrypt(n.Resource)
	if err != nil {
		return nil, fmt.Errorf("wechatpay notify: decrypt: %w", err)
	}
	var t Transaction
	if err := json.Unmarshal(plain, &t); err != nil {
		return nil, err
	}
	if t.Mchid != c.cfg.MchID || t.Appid != c.cfg.AppID {
		return nil, errors.New("wechatpay notify: merchant mismatch")
	}
	return t.trade(), nil
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// AckNotify 成功返回 204；失败返回 500 与错误码，微信支付会重发。
func (c *Client) AckNotify(w http.ResponseWriter, err error) {
	if err == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": "FAIL", "message": "处理失败"})
}
