// Package alipay 是支付宝开放平台（OpenAPI，RSA2 签名）的客户端：电脑网站支付、当面付扫码、查单、关单、
// 退款与异步通知。同时支持公钥模式与公钥证书模式。
package alipay

import (
	"context"
	"crypto/md5" //nolint:gosec // 证书序列号按支付宝规范使用 MD5，不用于安全目的
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/yx-token/token-market/market/internal/payment"
)

const (
	GatewayProd    = "https://openapi.alipay.com/gateway.do"
	GatewaySandbox = "https://openapi-sandbox.dl.alipaydev.com/gateway.do"
)

type Config struct {
	AppID      string
	Gateway    string
	PrivateKey *rsa.PrivateKey // 应用私钥
	// AlipayPublicKey 用于验签：公钥模式为支付宝公钥，证书模式取自支付宝公钥证书
	AlipayPublicKey *rsa.PublicKey
	// 证书模式下的应用公钥证书与支付宝根证书序列号；为空表示公钥模式
	AppCertSN        string
	AlipayRootCertSN string
	NotifyURL        string
	Channels         []string // payment.ChannelAlipayPage、payment.ChannelAlipayF2F
	HTTPClient       *http.Client
	Now              func() time.Time
}

type Client struct {
	cfg Config
}

func New(cfg Config) (*Client, error) {
	switch {
	case cfg.AppID == "":
		return nil, errors.New("alipay: app_id is required")
	case cfg.PrivateKey == nil:
		return nil, errors.New("alipay: app private key is required")
	case cfg.AlipayPublicKey == nil:
		return nil, errors.New("alipay: alipay public key (or alipay public key certificate) is required")
	case cfg.NotifyURL == "":
		return nil, errors.New("alipay: notify url is required (payment.public_url)")
	case (cfg.AppCertSN == "") != (cfg.AlipayRootCertSN == ""):
		return nil, errors.New("alipay: certificate mode needs both app certificate and alipay root certificate")
	}
	if cfg.Gateway == "" {
		cfg.Gateway = GatewayProd
	}
	if len(cfg.Channels) == 0 {
		cfg.Channels = []string{payment.ChannelAlipayPage, payment.ChannelAlipayF2F}
	}
	for _, ch := range cfg.Channels {
		if payment.ProviderOf(ch) != payment.ProviderAlipay {
			return nil, fmt.Errorf("alipay: unsupported channel %q", ch)
		}
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 15 * time.Second}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Client{cfg: cfg}, nil
}

func (c *Client) Name() string       { return payment.ProviderAlipay }
func (c *Client) Channels() []string { return c.cfg.Channels }

// CertMode 表示使用公钥证书模式。
func (c *Client) CertMode() bool { return c.cfg.AppCertSN != "" }

// ---- 签名 ----

// SignContent 按支付宝规则生成待签名串：去掉 exclude 中的参数与空值，按参数名 ASCII 升序以 k=v 用 & 连接。
func SignContent(v url.Values, exclude ...string) string {
	skip := map[string]bool{}
	for _, e := range exclude {
		skip[e] = true
	}
	keys := make([]string, 0, len(v))
	for k := range v {
		if !skip[k] && v.Get(k) != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(v.Get(k))
	}
	return b.String()
}

func (c *Client) params(method string) url.Values {
	v := url.Values{}
	v.Set("app_id", c.cfg.AppID)
	v.Set("method", method)
	v.Set("format", "JSON")
	v.Set("charset", "utf-8")
	v.Set("sign_type", "RSA2")
	v.Set("timestamp", c.cfg.Now().In(payment.China).Format("2006-01-02 15:04:05"))
	v.Set("version", "1.0")
	if c.CertMode() {
		v.Set("app_cert_sn", c.cfg.AppCertSN)
		v.Set("alipay_root_cert_sn", c.cfg.AlipayRootCertSN)
	}
	return v
}

func (c *Client) sign(v url.Values) error {
	s, err := payment.SignSHA256(c.cfg.PrivateKey, SignContent(v, "sign"))
	if err != nil {
		return err
	}
	v.Set("sign", s)
	return nil
}

// commonResp 是所有接口响应的公共字段。
type commonResp struct {
	Code    string `json:"code"`
	Msg     string `json:"msg"`
	SubCode string `json:"sub_code"`
	SubMsg  string `json:"sub_msg"`
}

// call 调用接口，校验响应签名并返回响应节点；业务失败返回 *payment.Error。
func (c *Client) call(ctx context.Context, method string, biz any, extra map[string]string) (json.RawMessage, error) {
	v := c.params(method)
	for k, val := range extra {
		v.Set(k, val)
	}
	b, err := json.Marshal(biz)
	if err != nil {
		return nil, err
	}
	v.Set("biz_content", string(b))
	if err := c.sign(v); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.Gateway, strings.NewReader(v.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=utf-8")
	resp, err := c.cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("alipay %s: %w", method, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("alipay %s: read response: %w", method, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("alipay %s: http %d", method, resp.StatusCode)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("alipay %s: decode response: %w", method, err)
	}
	node, ok := raw[strings.ReplaceAll(method, ".", "_")+"_response"]
	if !ok {
		node = raw["error_response"]
	}
	if node == nil {
		return nil, fmt.Errorf("alipay %s: unexpected response", method)
	}
	var common commonResp
	if err := json.Unmarshal(node, &common); err != nil {
		return nil, fmt.Errorf("alipay %s: decode response: %w", method, err)
	}
	var sig string
	if s, ok := raw["sign"]; ok {
		_ = json.Unmarshal(s, &sig)
	}
	switch {
	case sig != "":
		// 签名覆盖响应节点的原始 JSON 文本
		if err := payment.VerifySHA256(c.cfg.AlipayPublicKey, string(node), sig); err != nil {
			return nil, fmt.Errorf("alipay %s: response signature invalid: %w", method, err)
		}
	case common.Code == "10000":
		return nil, fmt.Errorf("alipay %s: response is not signed", method)
	}
	if common.Code != "10000" {
		code := common.SubCode
		if code == "" {
			code = common.Code
		}
		msg := common.SubMsg
		if msg == "" {
			msg = common.Msg
		}
		return nil, &payment.Error{Provider: payment.ProviderAlipay, Code: code, Message: msg}
	}
	return node, nil
}

func errCode(err error) string {
	var pe *payment.Error
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

func timeoutExpress(now, expire time.Time) string {
	m := int(expire.Sub(now).Minutes())
	if m < 1 {
		m = 1
	}
	return fmt.Sprintf("%dm", m)
}

// ---- 下单 ----

func (c *Client) Create(ctx context.Context, channel string, o payment.Order) (*payment.Intent, error) {
	biz := map[string]any{
		"out_trade_no":    o.OutTradeNo,
		"total_amount":    payment.Yuan(o.AmountFen),
		"subject":         o.Subject,
		"timeout_express": timeoutExpress(c.cfg.Now(), o.ExpireAt),
	}
	switch channel {
	case payment.ChannelAlipayPage:
		// 电脑网站支付：签名后的参数拼成收银台地址，由浏览器跳转
		biz["product_code"] = "FAST_INSTANT_TRADE_PAY"
		v := c.params("alipay.trade.page.pay")
		v.Set("notify_url", c.cfg.NotifyURL)
		if o.ReturnURL != "" {
			v.Set("return_url", o.ReturnURL)
		}
		b, _ := json.Marshal(biz)
		v.Set("biz_content", string(b))
		if err := c.sign(v); err != nil {
			return nil, err
		}
		return &payment.Intent{Kind: "redirect", URL: c.cfg.Gateway + "?" + v.Encode()}, nil
	case payment.ChannelAlipayF2F:
		// 当面付（扫码支付）：预下单拿到二维码内容
		node, err := c.call(ctx, "alipay.trade.precreate", biz, map[string]string{"notify_url": c.cfg.NotifyURL})
		if err != nil {
			return nil, err
		}
		var r struct {
			QRCode string `json:"qr_code"`
		}
		if err := json.Unmarshal(node, &r); err != nil || r.QRCode == "" {
			return nil, errors.New("alipay precreate: missing qr_code")
		}
		return &payment.Intent{Kind: "qrcode", QRCode: r.QRCode}, nil
	}
	return nil, fmt.Errorf("alipay: unsupported channel %q", channel)
}

// ---- 查单与关单 ----

func (c *Client) Query(ctx context.Context, outTradeNo string) (*payment.Trade, error) {
	node, err := c.call(ctx, "alipay.trade.query", map[string]string{"out_trade_no": outTradeNo}, nil)
	if err != nil {
		if errCode(err) == "ACQ.TRADE_NOT_EXIST" {
			return nil, payment.ErrTradeNotFound
		}
		return nil, err
	}
	var r struct {
		TradeNo     string `json:"trade_no"`
		OutTradeNo  string `json:"out_trade_no"`
		TradeStatus string `json:"trade_status"`
		TotalAmount string `json:"total_amount"`
		SendPayDate string `json:"send_pay_date"`
	}
	if err := json.Unmarshal(node, &r); err != nil {
		return nil, err
	}
	t := &payment.Trade{OutTradeNo: outTradeNo, TradeNo: r.TradeNo, State: tradeState(r.TradeStatus)}
	if r.TotalAmount != "" {
		if t.AmountFen, err = payment.Fen(r.TotalAmount); err != nil {
			return nil, err
		}
	}
	t.PaidAt = parseTime(r.SendPayDate)
	return t, nil
}

func tradeState(s string) payment.TradeState {
	switch s {
	case "TRADE_SUCCESS", "TRADE_FINISHED":
		return payment.TradePaid
	case "TRADE_CLOSED":
		return payment.TradeClosed
	}
	return payment.TradePending
}

func parseTime(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.ParseInLocation("2006-01-02 15:04:05", s, payment.China)
	if err != nil {
		return nil
	}
	return &t
}

func (c *Client) Close(ctx context.Context, outTradeNo string) error {
	_, err := c.call(ctx, "alipay.trade.close", map[string]string{"out_trade_no": outTradeNo}, nil)
	if errCode(err) == "ACQ.TRADE_NOT_EXIST" {
		return nil // 当面付用户未扫码时支付宝侧没有交易
	}
	return err
}

// ---- 退款 ----

// retryable 是可以原样重试的系统错误。
func retryable(code string) bool {
	switch code {
	case "ACQ.SYSTEM_ERROR", "aop.ACQ.SYSTEM_ERROR", "isp.unknow-error", "ACQ.REFUND_FEE_ERROR_CONCURRENT":
		return true
	}
	return false
}

func (c *Client) Refund(ctx context.Context, r payment.Refund) (*payment.RefundResult, error) {
	biz := map[string]string{
		"out_trade_no":   r.OutTradeNo,
		"refund_amount":  payment.Yuan(r.AmountFen),
		"out_request_no": r.OutRefundNo,
		"refund_reason":  r.Reason,
	}
	node, err := c.call(ctx, "alipay.trade.refund", biz, nil)
	if err != nil {
		var pe *payment.Error
		if errors.As(err, &pe) && !retryable(pe.Code) {
			return &payment.RefundResult{State: payment.RefundFailed, Message: pe.Error()}, nil
		}
		return nil, err
	}
	var res struct {
		TradeNo    string `json:"trade_no"`
		FundChange string `json:"fund_change"`
	}
	if err := json.Unmarshal(node, &res); err != nil {
		return nil, err
	}
	if res.FundChange == "Y" {
		return &payment.RefundResult{State: payment.RefundSuccess, RefundNo: res.TradeNo}, nil
	}
	// fund_change = N：可能是同一退款单号的重复请求，以退款查询为准
	return &payment.RefundResult{State: payment.RefundProcessing, RefundNo: res.TradeNo}, nil
}

func (c *Client) QueryRefund(ctx context.Context, outTradeNo, outRefundNo string) (*payment.RefundResult, error) {
	node, err := c.call(ctx, "alipay.trade.fastpay.refund.query",
		map[string]string{"out_trade_no": outTradeNo, "out_request_no": outRefundNo}, nil)
	if err != nil {
		if errCode(err) == "ACQ.TRADE_NOT_EXIST" {
			return nil, payment.ErrRefundNotFound
		}
		return nil, err
	}
	var res struct {
		TradeNo      string `json:"trade_no"`
		RefundStatus string `json:"refund_status"`
	}
	if err := json.Unmarshal(node, &res); err != nil {
		return nil, err
	}
	// 未返回 refund_status 表示支付宝没有收到这笔退款或退款失败，按同一单号重新发起退款是幂等的
	if res.RefundStatus != "REFUND_SUCCESS" {
		return nil, payment.ErrRefundNotFound
	}
	return &payment.RefundResult{State: payment.RefundSuccess, RefundNo: res.TradeNo}, nil
}

// ---- 异步通知 ----

func (c *Client) ParseNotify(r *http.Request) (*payment.Trade, error) {
	if err := r.ParseForm(); err != nil {
		return nil, fmt.Errorf("alipay notify: %w", err)
	}
	v := r.PostForm
	sig := v.Get("sign")
	if sig == "" {
		return nil, errors.New("alipay notify: missing sign")
	}
	if err := payment.VerifySHA256(c.cfg.AlipayPublicKey, SignContent(v, "sign", "sign_type"), sig); err != nil {
		return nil, fmt.Errorf("alipay notify: signature invalid: %w", err)
	}
	if v.Get("app_id") != c.cfg.AppID {
		return nil, errors.New("alipay notify: app_id mismatch")
	}
	t := &payment.Trade{OutTradeNo: v.Get("out_trade_no"), TradeNo: v.Get("trade_no"), State: tradeState(v.Get("trade_status"))}
	if t.OutTradeNo == "" {
		return nil, errors.New("alipay notify: missing out_trade_no")
	}
	if amt := v.Get("total_amount"); amt != "" {
		fen, err := payment.Fen(amt)
		if err != nil {
			return nil, err
		}
		t.AmountFen = fen
	}
	t.PaidAt = parseTime(v.Get("gmt_payment"))
	return t, nil
}

// AckNotify 支付宝要求处理成功时返回纯文本 success，否则会按策略重发。
func (c *Client) AckNotify(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if err != nil {
		_, _ = io.WriteString(w, "fail")
		return
	}
	_, _ = io.WriteString(w, "success")
}

// ---- 证书模式 ----

// CertSN 计算证书序列号：MD5(颁发者 DN + 十进制序列号)。
func CertSN(c *x509.Certificate) string {
	sum := md5.Sum([]byte(c.Issuer.String() + c.SerialNumber.String())) //nolint:gosec // 支付宝规范
	return hex.EncodeToString(sum[:])
}

// RootCertSN 计算支付宝根证书序列号：根证书文件中每个 RSA 签名证书的序列号用 _ 连接（跳过国密证书）。
func RootCertSN(pemData string) (string, error) {
	var sns []string
	rest := []byte(pemData)
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue // 国密（SM2）证书 Go 无法解析，按规范本来也要跳过
		}
		if cert.SignatureAlgorithm == x509.SHA256WithRSA || cert.SignatureAlgorithm == x509.SHA1WithRSA {
			sns = append(sns, CertSN(cert))
		}
	}
	if len(sns) == 0 {
		return "", errors.New("alipay root certificate: no RSA certificate found")
	}
	return strings.Join(sns, "_"), nil
}
