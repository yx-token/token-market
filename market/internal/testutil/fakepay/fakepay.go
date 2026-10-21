// Package fakepay 是支付宝与微信支付的替身，用于测试与本地开发：按真实协议校验请求签名、对应答与通知签名，
// 提供模拟收银台与"扫码付款"页面。不能用于生产。
package fakepay

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yx-token/token-market/market/internal/payment"
	"github.com/yx-token/token-market/market/internal/payment/alipay"
	"github.com/yx-token/token-market/market/internal/platform/randx"
)

// Keys 是替身与商户两侧的密钥。替身持有平台私钥，用商户公钥验签。
type Keys struct {
	AlipayPlatform  *rsa.PrivateKey // 支付宝平台密钥（商户侧配置其公钥）
	AlipayApp       *rsa.PublicKey  // 商户应用公钥
	WechatPlatform  *rsa.PrivateKey // 微信支付公钥 / 平台证书的私钥（商户侧配置其公钥）
	WechatMerchant  *rsa.PublicKey  // 商户 API 证书公钥
	WechatAPIv3Key  string
	WechatPublicKey string // 微信支付公钥 ID
}

// GenerateKeys 生成一套完整的测试密钥，返回替身用的 Keys 与商户侧的私钥。
func GenerateKeys() (Keys, *rsa.PrivateKey, *rsa.PrivateKey, error) {
	gen := func() (*rsa.PrivateKey, error) { return rsa.GenerateKey(rand.Reader, 2048) }
	alipayPlatform, err := gen()
	if err != nil {
		return Keys{}, nil, nil, err
	}
	app, err := gen()
	if err != nil {
		return Keys{}, nil, nil, err
	}
	wechatPlatform, err := gen()
	if err != nil {
		return Keys{}, nil, nil, err
	}
	merchant, err := gen()
	if err != nil {
		return Keys{}, nil, nil, err
	}
	return Keys{
		AlipayPlatform: alipayPlatform, AlipayApp: &app.PublicKey,
		WechatPlatform: wechatPlatform, WechatMerchant: &merchant.PublicKey,
		WechatAPIv3Key: "fakeapiv3keyfakeapiv3keyfakeapiv", WechatPublicKey: "PUB_KEY_ID_FAKE0001",
	}, app, merchant, nil
}

type Config struct {
	Keys            Keys
	AlipayAppID     string
	WechatAppID     string
	WechatMchID     string
	WechatMchSerial string // 商户证书序列号，校验 Authorization 头
	// WechatCertMode 为 true 时用平台证书签名并提供 /v3/certificates，否则用微信支付公钥签名
	WechatCertMode bool
	// PublicURL 是替身对外的地址（收银台与二维码链接），为空时使用 httptest 地址
	PublicURL string
	Log       *slog.Logger
}

type trade struct {
	provider   string
	outTradeNo string
	amountFen  int64
	subject    string
	state      payment.TradeState
	scanned    bool // 当面付：用户扫码后支付宝侧才有交易
	notifyURL  string
	returnURL  string
	tradeNo    string
	paidAt     time.Time
	expireAt   time.Time
	token      string
	refunds    map[string]int64
	refunded   int64
}

type Server struct {
	cfg  Config
	HTTP *httptest.Server
	h    http.Handler
	log  *slog.Logger

	mu         sync.Mutex
	trades     map[string]*trade // out_trade_no
	byToken    map[string]*trade
	dropNotify bool
	notified   []string
	certSerial string
	certPEM    string
	client     *http.Client
}

// New 启动替身（httptest）。
func New(cfg Config) *Server {
	s := NewUnstarted(cfg)
	s.HTTP = httptest.NewServer(s.h)
	if s.cfg.PublicURL == "" {
		s.cfg.PublicURL = s.HTTP.URL
	}
	return s
}

// NewUnstarted 只构造处理器（开发环境由调用方监听）。
func NewUnstarted(cfg Config) *Server {
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	s := &Server{cfg: cfg, log: cfg.Log, trades: map[string]*trade{}, byToken: map[string]*trade{},
		client: &http.Client{Timeout: 10 * time.Second}}
	s.certSerial, s.certPEM = selfSigned(cfg.Keys.WechatPlatform)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /gateway.do", s.alipayPage)
	mux.HandleFunc("POST /gateway.do", s.alipayAPI)
	mux.HandleFunc("POST /fakepay/pay", s.payForm)
	mux.HandleFunc("GET /fakepay/qr/{token}", s.scanPage)
	mux.HandleFunc("POST /v3/pay/transactions/native", s.wechatNative)
	mux.HandleFunc("GET /v3/pay/transactions/out-trade-no/{no}", s.wechatQuery)
	mux.HandleFunc("POST /v3/pay/transactions/out-trade-no/{no}/close", s.wechatClose)
	mux.HandleFunc("POST /v3/refund/domestic/refunds", s.wechatRefund)
	mux.HandleFunc("GET /v3/refund/domestic/refunds/{no}", s.wechatRefundQuery)
	mux.HandleFunc("GET /v3/certificates", s.wechatCerts)
	s.h = mux
	return s
}

func (s *Server) Handler() http.Handler { return s.h }

func (s *Server) URL() string { return s.cfg.PublicURL }

// SetPublicURL 设置收银台与二维码链接使用的地址（开发环境监听后调用）。
func (s *Server) SetPublicURL(u string) { s.cfg.PublicURL = u }

func (s *Server) Close() {
	if s.HTTP != nil {
		s.HTTP.Close()
	}
}

// AlipayGateway 与 WechatBaseURL 是商户侧应配置的地址。
func (s *Server) AlipayGateway() string { return s.cfg.PublicURL + "/gateway.do" }
func (s *Server) WechatBaseURL() string { return s.cfg.PublicURL }

// WechatPlatformCert 返回平台证书 PEM（证书模式）。
func (s *Server) WechatPlatformCert() string { return s.certPEM }

// SetDropNotify 为 true 时付款后不发异步通知（模拟通知丢失，测试主动查单）。
func (s *Server) SetDropNotify(v bool) {
	s.mu.Lock()
	s.dropNotify = v
	s.mu.Unlock()
}

// Notified 返回已成功送达的通知（out_trade_no）。
func (s *Server) Notified() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.notified...)
}

// TradeState 返回交易状态与累计退款（分）。
func (s *Server) TradeState(outTradeNo string) (payment.TradeState, int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.trades[outTradeNo]
	if t == nil {
		return "", 0, false
	}
	return t.state, t.refunded, true
}

// Pay 模拟用户完成付款（扫码或在收银台确认），并发送异步通知。
func (s *Server) Pay(outTradeNo string) error {
	s.mu.Lock()
	t := s.trades[outTradeNo]
	s.mu.Unlock()
	if t == nil {
		return errors.New("trade not found")
	}
	return s.pay(t)
}

// TokenOf 返回二维码 / 收银台链接中的令牌对应的商户单号（测试用）。
func (s *Server) TokenOf(link string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for tok, t := range s.byToken {
		if strings.Contains(link, tok) {
			return t.outTradeNo
		}
	}
	return ""
}

func (s *Server) pay(t *trade) error {
	s.mu.Lock()
	switch {
	case t.state == payment.TradeClosed:
		s.mu.Unlock()
		return errors.New("交易已关闭")
	case !t.expireAt.IsZero() && time.Now().After(t.expireAt):
		t.state = payment.TradeClosed
		s.mu.Unlock()
		return errors.New("交易已超时关闭")
	case t.state != payment.TradePaid:
		t.state, t.scanned, t.paidAt = payment.TradePaid, true, time.Now()
		if t.provider == payment.ProviderAlipay {
			t.tradeNo = "2026" + randx.String(24)
		} else {
			t.tradeNo = "4200" + randx.String(24)
		}
	}
	drop := s.dropNotify
	s.mu.Unlock()
	if drop {
		return nil
	}
	var err error
	if t.provider == payment.ProviderAlipay {
		err = s.alipayNotify(t)
	} else {
		err = s.wechatNotify(t)
	}
	if err != nil {
		s.log.Warn("fakepay notify failed", "out_trade_no", t.outTradeNo, "err", err)
		return nil // 真实渠道会稍后重发；商户侧也会主动查单
	}
	s.mu.Lock()
	s.notified = append(s.notified, t.outTradeNo)
	s.mu.Unlock()
	return nil
}

func (s *Server) newTrade(provider, outTradeNo string, fen int64, subject, notifyURL string, expire time.Time) (*trade, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t := s.trades[outTradeNo]; t != nil {
		if t.amountFen != fen {
			return nil, errors.New("订单号重复且金额不一致")
		}
		return t, nil
	}
	t := &trade{provider: provider, outTradeNo: outTradeNo, amountFen: fen, subject: subject, state: payment.TradePending,
		notifyURL: notifyURL, token: randx.String(24), refunds: map[string]int64{}, expireAt: expire}
	s.trades[outTradeNo] = t
	s.byToken[t.token] = t
	return t, nil
}

// ---- 页面 ----

var pageTmpl = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><title>{{.Title}}</title>
<style>body{font-family:system-ui,sans-serif;background:#f5f6f8;margin:0}main{max-width:420px;margin:60px auto;background:#fff;border-radius:12px;padding:28px;box-shadow:0 2px 12px rgba(0,0,0,.08)}
h1{font-size:18px;margin:0 0 4px}.sub{color:#888;font-size:12px;margin-bottom:20px}.amt{font-size:32px;font-weight:600;margin:16px 0}
.row{display:flex;justify-content:space-between;font-size:14px;color:#555;margin:6px 0}button{width:100%;padding:12px;border:0;border-radius:8px;color:#fff;font-size:16px;cursor:pointer;margin-top:20px}
.alipay{background:#1677ff}.wechat{background:#07c160}.ok{color:#07c160;font-size:16px;margin-top:20px}.err{color:#d4380d;margin-top:20px}</style></head>
<body><main><h1>{{.Title}}</h1><div class="sub">开发替身，不会产生真实扣款</div>
{{if .HasTrade}}<div class="amt">¥{{.Amount}}</div><div class="row"><span>商品</span><span>{{.Subject}}</span></div>
<div class="row"><span>商户订单号</span><span>{{.OutTradeNo}}</span></div>
{{if .Paid}}<div class="ok" id="paid">付款成功</div>{{else if .Closed}}<div class="err">交易已关闭</div>{{else}}
<form method="post" action="/fakepay/pay"><input type="hidden" name="token" value="{{.Token}}">
<button class="{{.Class}}" type="submit" id="confirm-pay">{{.Button}}</button></form>{{end}}{{end}}
{{if .Error}}<div class="err">{{.Error}}</div>{{end}}</main></body></html>`))

type pageData struct {
	Title, Amount, Class, Button, Error string
	Subject, OutTradeNo, Token          string
	HasTrade, Paid, Closed              bool
}

func (s *Server) render(w http.ResponseWriter, t *trade, errMsg string) {
	d := pageData{Error: errMsg}
	if t != nil {
		s.mu.Lock()
		cp := *t
		s.mu.Unlock()
		d.HasTrade, d.Amount, d.Paid, d.Closed = true, payment.Yuan(cp.amountFen), cp.state == payment.TradePaid, cp.state == payment.TradeClosed
		d.Subject, d.OutTradeNo, d.Token = cp.subject, cp.outTradeNo, cp.token
		if cp.provider == payment.ProviderAlipay {
			d.Title, d.Class, d.Button = "支付宝收银台", "alipay", "确认付款"
			if cp.returnURL == "" {
				d.Title = "支付宝扫码付款"
			}
		} else {
			d.Title, d.Class, d.Button = "微信支付", "wechat", "确认支付"
		}
	} else {
		d.Title = "支付"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pageTmpl.Execute(w, d)
}

// scanPage 是二维码内容指向的页面：用浏览器打开相当于用 App 扫码。
func (s *Server) scanPage(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	t := s.byToken[r.PathValue("token")]
	if t != nil {
		t.scanned = true
	}
	s.mu.Unlock()
	if t == nil {
		w.WriteHeader(http.StatusNotFound)
		s.render(w, nil, "二维码无效")
		return
	}
	s.render(w, t, "")
}

func (s *Server) payForm(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	t := s.byToken[r.FormValue("token")]
	s.mu.Unlock()
	if t == nil {
		w.WriteHeader(http.StatusNotFound)
		s.render(w, nil, "交易不存在")
		return
	}
	if err := s.pay(t); err != nil {
		s.render(w, t, err.Error())
		return
	}
	if t.provider == payment.ProviderAlipay && t.returnURL != "" {
		http.Redirect(w, r, s.alipayReturnURL(t), http.StatusFound)
		return
	}
	s.render(w, t, "")
}

// ---- 支付宝 ----

func (s *Server) alipayVerify(v url.Values) error {
	if v.Get("app_id") != s.cfg.AlipayAppID {
		return errors.New("invalid app_id")
	}
	if v.Get("sign_type") != "RSA2" {
		return errors.New("sign_type must be RSA2")
	}
	return payment.VerifySHA256(s.cfg.Keys.AlipayApp, alipay.SignContent(v, "sign"), v.Get("sign"))
}

type alipayBiz struct {
	OutTradeNo     string `json:"out_trade_no"`
	TotalAmount    string `json:"total_amount"`
	Subject        string `json:"subject"`
	TimeoutExpress string `json:"timeout_express"`
	RefundAmount   string `json:"refund_amount"`
	OutRequestNo   string `json:"out_request_no"`
}

func expireFrom(timeout string) time.Time {
	if m, err := strconv.Atoi(strings.TrimSuffix(timeout, "m")); err == nil && m > 0 {
		return time.Now().Add(time.Duration(m) * time.Minute)
	}
	return time.Time{}
}

func (s *Server) alipayPage(w http.ResponseWriter, r *http.Request) {
	v := r.URL.Query()
	if err := s.alipayVerify(v); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		s.render(w, nil, "签名校验失败："+err.Error())
		return
	}
	if v.Get("method") != "alipay.trade.page.pay" {
		w.WriteHeader(http.StatusBadRequest)
		s.render(w, nil, "不支持的接口")
		return
	}
	var biz alipayBiz
	if err := json.Unmarshal([]byte(v.Get("biz_content")), &biz); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		s.render(w, nil, "biz_content 无效")
		return
	}
	fen, err := payment.Fen(biz.TotalAmount)
	if err != nil {
		s.render(w, nil, err.Error())
		return
	}
	t, err := s.newTrade(payment.ProviderAlipay, biz.OutTradeNo, fen, biz.Subject, v.Get("notify_url"), expireFrom(biz.TimeoutExpress))
	if err != nil {
		s.render(w, nil, err.Error())
		return
	}
	s.mu.Lock()
	t.scanned, t.returnURL = true, v.Get("return_url")
	s.mu.Unlock()
	s.render(w, t, "")
}

func (s *Server) alipayRespond(w http.ResponseWriter, method string, node map[string]any) {
	if _, ok := node["code"]; !ok {
		node["code"], node["msg"] = "10000", "Success"
	}
	raw, _ := json.Marshal(node)
	sig, _ := payment.SignSHA256(s.cfg.Keys.AlipayPlatform, string(raw))
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = fmt.Fprintf(w, `{"%s_response":%s,"sign":"%s"}`, strings.ReplaceAll(method, ".", "_"), raw, sig)
}

func alipayFail(sub, msg string) map[string]any {
	return map[string]any{"code": "40004", "msg": "Business Failed", "sub_code": sub, "sub_msg": msg}
}

func (s *Server) alipayAPI(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	v := r.PostForm
	method := v.Get("method")
	if err := s.alipayVerify(v); err != nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = fmt.Fprintf(w, `{"error_response":{"code":"40002","msg":"Invalid Arguments","sub_code":"isv.invalid-signature","sub_msg":%q}}`, err.Error())
		return
	}
	var biz alipayBiz
	_ = json.Unmarshal([]byte(v.Get("biz_content")), &biz)
	s.alipayRespond(w, method, s.alipayDispatch(method, biz, v))
}

func (s *Server) alipayDispatch(method string, biz alipayBiz, v url.Values) map[string]any {
	s.mu.Lock()
	t := s.trades[biz.OutTradeNo]
	s.mu.Unlock()
	switch method {
	case "alipay.trade.precreate":
		fen, err := payment.Fen(biz.TotalAmount)
		if err != nil {
			return alipayFail("ACQ.INVALID_PARAMETER", err.Error())
		}
		t, err := s.newTrade(payment.ProviderAlipay, biz.OutTradeNo, fen, biz.Subject, v.Get("notify_url"), expireFrom(biz.TimeoutExpress))
		if err != nil {
			return alipayFail("ACQ.CONTEXT_INCONSISTENT", err.Error())
		}
		return map[string]any{"out_trade_no": t.outTradeNo, "qr_code": s.cfg.PublicURL + "/fakepay/qr/" + t.token}
	case "alipay.trade.query":
		if t == nil || !t.scanned {
			return alipayFail("ACQ.TRADE_NOT_EXIST", "交易不存在")
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		status := map[payment.TradeState]string{payment.TradePending: "WAIT_BUYER_PAY", payment.TradePaid: "TRADE_SUCCESS",
			payment.TradeClosed: "TRADE_CLOSED"}[t.state]
		out := map[string]any{"out_trade_no": t.outTradeNo, "trade_no": t.tradeNo, "trade_status": status, "total_amount": payment.Yuan(t.amountFen)}
		if !t.paidAt.IsZero() {
			out["send_pay_date"] = t.paidAt.In(payment.China).Format("2006-01-02 15:04:05")
		}
		return out
	case "alipay.trade.close":
		if t == nil || !t.scanned {
			return alipayFail("ACQ.TRADE_NOT_EXIST", "交易不存在")
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if t.state == payment.TradePaid {
			return alipayFail("ACQ.TRADE_STATUS_ERROR", "交易状态不合法")
		}
		t.state = payment.TradeClosed
		return map[string]any{"out_trade_no": t.outTradeNo}
	case "alipay.trade.refund":
		if t == nil || t.state != payment.TradePaid {
			return alipayFail("ACQ.TRADE_STATUS_ERROR", "交易状态不合法")
		}
		fen, err := payment.Fen(biz.RefundAmount)
		if err != nil {
			return alipayFail("ACQ.INVALID_PARAMETER", err.Error())
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if prev, ok := t.refunds[biz.OutRequestNo]; ok {
			if prev != fen {
				return alipayFail("ACQ.REFUND_AMT_NOT_EQUAL_TOTAL", "退款金额与原请求不一致")
			}
			return map[string]any{"trade_no": t.tradeNo, "out_trade_no": t.outTradeNo, "fund_change": "N", "refund_fee": payment.Yuan(t.refunded)}
		}
		if t.refunded+fen > t.amountFen {
			return alipayFail("ACQ.REFUND_AMT_NOT_EQUAL_TOTAL", "退款金额超限")
		}
		t.refunds[biz.OutRequestNo] = fen
		t.refunded += fen
		return map[string]any{"trade_no": t.tradeNo, "out_trade_no": t.outTradeNo, "fund_change": "Y", "refund_fee": payment.Yuan(t.refunded)}
	case "alipay.trade.fastpay.refund.query":
		if t == nil {
			return alipayFail("ACQ.TRADE_NOT_EXIST", "交易不存在")
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		fen, ok := t.refunds[biz.OutRequestNo]
		if !ok {
			return map[string]any{"trade_no": t.tradeNo, "out_trade_no": t.outTradeNo}
		}
		return map[string]any{"trade_no": t.tradeNo, "out_trade_no": t.outTradeNo, "out_request_no": biz.OutRequestNo,
			"refund_amount": payment.Yuan(fen), "refund_status": "REFUND_SUCCESS"}
	}
	return map[string]any{"code": "40004", "msg": "Business Failed", "sub_code": "isv.invalid-method", "sub_msg": "不支持的接口 " + method}
}

func (s *Server) alipaySigned(t *trade, extra url.Values, method string) url.Values {
	s.mu.Lock()
	v := url.Values{}
	v.Set("app_id", s.cfg.AlipayAppID)
	v.Set("charset", "utf-8")
	v.Set("version", "1.0")
	v.Set("sign_type", "RSA2")
	v.Set("out_trade_no", t.outTradeNo)
	v.Set("trade_no", t.tradeNo)
	v.Set("total_amount", payment.Yuan(t.amountFen))
	if method != "" {
		v.Set("method", method)
		v.Set("timestamp", time.Now().In(payment.China).Format("2006-01-02 15:04:05"))
	}
	s.mu.Unlock()
	for k, vs := range extra {
		v[k] = vs
	}
	sig, _ := payment.SignSHA256(s.cfg.Keys.AlipayPlatform, alipay.SignContent(v, "sign", "sign_type"))
	v.Set("sign", sig)
	return v
}

func (s *Server) alipayNotify(t *trade) error {
	s.mu.Lock()
	extra := url.Values{}
	extra.Set("notify_time", time.Now().In(payment.China).Format("2006-01-02 15:04:05"))
	extra.Set("notify_type", "trade_status_sync")
	extra.Set("notify_id", randx.String(32))
	extra.Set("trade_status", "TRADE_SUCCESS")
	extra.Set("receipt_amount", payment.Yuan(t.amountFen))
	extra.Set("buyer_pay_amount", payment.Yuan(t.amountFen))
	extra.Set("subject", t.subject)
	extra.Set("gmt_payment", t.paidAt.In(payment.China).Format("2006-01-02 15:04:05"))
	notifyURL := t.notifyURL
	s.mu.Unlock()
	v := s.alipaySigned(t, extra, "")
	resp, err := s.client.PostForm(notifyURL, v)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if strings.TrimSpace(string(body)) != "success" {
		return fmt.Errorf("merchant replied %q", body)
	}
	return nil
}

func (s *Server) alipayReturnURL(t *trade) string {
	v := s.alipaySigned(t, nil, "alipay.trade.page.pay.return")
	sep := "?"
	if strings.Contains(t.returnURL, "?") {
		sep = "&"
	}
	return t.returnURL + sep + v.Encode()
}

// ---- 微信支付 ----

func (s *Server) wechatVerify(r *http.Request, body []byte) error {
	auth, ok := strings.CutPrefix(r.Header.Get("Authorization"), "WECHATPAY2-SHA256-RSA2048 ")
	if !ok {
		return errors.New("invalid authorization scheme")
	}
	fields := map[string]string{}
	for _, part := range strings.Split(auth, ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
		fields[k] = strings.Trim(v, `"`)
	}
	if fields["mchid"] != s.cfg.WechatMchID || fields["serial_no"] != s.cfg.WechatMchSerial {
		return errors.New("merchant id or certificate serial mismatch")
	}
	msg := r.Method + "\n" + r.URL.RequestURI() + "\n" + fields["timestamp"] + "\n" + fields["nonce_str"] + "\n" + string(body) + "\n"
	return payment.VerifySHA256(s.cfg.Keys.WechatMerchant, msg, fields["signature"])
}

func (s *Server) wechatSign(h http.Header, body []byte) {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := randx.String(32)
	sig, _ := payment.SignSHA256(s.cfg.Keys.WechatPlatform, ts+"\n"+nonce+"\n"+string(body)+"\n")
	serial := s.cfg.Keys.WechatPublicKey
	if s.cfg.WechatCertMode {
		serial = s.certSerial
	}
	h.Set("Wechatpay-Serial", serial)
	h.Set("Wechatpay-Timestamp", ts)
	h.Set("Wechatpay-Nonce", nonce)
	h.Set("Wechatpay-Signature", sig)
}

func (s *Server) wechatJSON(w http.ResponseWriter, status int, v any) {
	var body []byte
	if v != nil {
		body, _ = json.Marshal(v)
	}
	s.wechatSign(w.Header(), body)
	if v != nil {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func (s *Server) wechatErr(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "message": msg})
}

// wechatAuth 读取请求体并校验签名，失败时已写出应答。
func (s *Server) wechatAuth(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	body, _ := io.ReadAll(r.Body)
	if err := s.wechatVerify(r, body); err != nil {
		s.wechatErr(w, http.StatusUnauthorized, "SIGN_ERROR", err.Error())
		return nil, false
	}
	return body, true
}

func (s *Server) wechatNative(w http.ResponseWriter, r *http.Request) {
	body, ok := s.wechatAuth(w, r)
	if !ok {
		return
	}
	var req struct {
		AppID       string `json:"appid"`
		MchID       string `json:"mchid"`
		Description string `json:"description"`
		OutTradeNo  string `json:"out_trade_no"`
		TimeExpire  string `json:"time_expire"`
		NotifyURL   string `json:"notify_url"`
		Amount      struct {
			Total int64 `json:"total"`
		} `json:"amount"`
	}
	if err := json.Unmarshal(body, &req); err != nil || req.OutTradeNo == "" || req.Amount.Total <= 0 {
		s.wechatErr(w, http.StatusBadRequest, "PARAM_ERROR", "invalid request")
		return
	}
	if req.AppID != s.cfg.WechatAppID || req.MchID != s.cfg.WechatMchID {
		s.wechatErr(w, http.StatusBadRequest, "APPID_MCHID_NOT_MATCH", "appid 与 mchid 不匹配")
		return
	}
	expire, _ := time.Parse(time.RFC3339, req.TimeExpire)
	t, err := s.newTrade(payment.ProviderWechat, req.OutTradeNo, req.Amount.Total, req.Description, req.NotifyURL, expire)
	if err != nil {
		s.wechatErr(w, http.StatusBadRequest, "OUT_TRADE_NO_USED", err.Error())
		return
	}
	s.wechatJSON(w, http.StatusOK, map[string]string{"code_url": s.cfg.PublicURL + "/fakepay/qr/" + t.token})
}

func (s *Server) transaction(t *trade) map[string]any {
	state := map[payment.TradeState]string{payment.TradePending: "NOTPAY", payment.TradePaid: "SUCCESS", payment.TradeClosed: "CLOSED"}[t.state]
	if t.state == payment.TradePaid && t.refunded > 0 {
		state = "REFUND"
	}
	out := map[string]any{"appid": s.cfg.WechatAppID, "mchid": s.cfg.WechatMchID, "out_trade_no": t.outTradeNo, "trade_state": state,
		"trade_type": "NATIVE", "amount": map[string]any{"total": t.amountFen, "payer_total": t.amountFen, "currency": "CNY"}}
	if t.tradeNo != "" {
		out["transaction_id"] = t.tradeNo
	}
	if !t.paidAt.IsZero() {
		out["success_time"] = t.paidAt.In(payment.China).Format(time.RFC3339)
	}
	return out
}

func (s *Server) wechatQuery(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.wechatAuth(w, r); !ok {
		return
	}
	s.mu.Lock()
	t := s.trades[r.PathValue("no")]
	var out map[string]any
	if t != nil && t.provider == payment.ProviderWechat {
		out = s.transaction(t)
	}
	s.mu.Unlock()
	if out == nil {
		s.wechatErr(w, http.StatusNotFound, "ORDER_NOT_EXIST", "订单不存在")
		return
	}
	s.wechatJSON(w, http.StatusOK, out)
}

func (s *Server) wechatClose(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.wechatAuth(w, r); !ok {
		return
	}
	s.mu.Lock()
	t := s.trades[r.PathValue("no")]
	paid := t != nil && t.state == payment.TradePaid
	if t != nil && !paid {
		t.state = payment.TradeClosed
	}
	s.mu.Unlock()
	switch {
	case t == nil:
		s.wechatErr(w, http.StatusNotFound, "ORDER_NOT_EXIST", "订单不存在")
	case paid:
		s.wechatErr(w, http.StatusBadRequest, "ORDERPAID", "订单已支付")
	default:
		s.wechatJSON(w, http.StatusNoContent, nil)
	}
}

func (s *Server) wechatRefund(w http.ResponseWriter, r *http.Request) {
	body, ok := s.wechatAuth(w, r)
	if !ok {
		return
	}
	var req struct {
		OutTradeNo  string `json:"out_trade_no"`
		OutRefundNo string `json:"out_refund_no"`
		Amount      struct {
			Refund int64 `json:"refund"`
			Total  int64 `json:"total"`
		} `json:"amount"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		s.wechatErr(w, http.StatusBadRequest, "PARAM_ERROR", "invalid request")
		return
	}
	s.mu.Lock()
	t := s.trades[req.OutTradeNo]
	var code, msg string
	switch {
	case t == nil || t.state != payment.TradePaid:
		code, msg = "RESOURCE_NOT_EXISTS", "订单不存在或未支付"
	case req.Amount.Total != t.amountFen:
		code, msg = "PARAM_ERROR", "订单金额不一致"
	case t.refunds[req.OutRefundNo] != 0 && t.refunds[req.OutRefundNo] != req.Amount.Refund:
		code, msg = "PARAM_ERROR", "退款金额与原请求不一致"
	case t.refunds[req.OutRefundNo] == 0 && t.refunded+req.Amount.Refund > t.amountFen:
		code, msg = "NOT_ENOUGH", "可退金额不足"
	default:
		if t.refunds[req.OutRefundNo] == 0 {
			t.refunds[req.OutRefundNo] = req.Amount.Refund
			t.refunded += req.Amount.Refund
		}
	}
	s.mu.Unlock()
	if code != "" {
		s.wechatErr(w, http.StatusBadRequest, code, msg)
		return
	}
	// 与真实接口一致：受理后处于退款中，查询时成功
	s.wechatJSON(w, http.StatusOK, map[string]any{"refund_id": "50" + req.OutRefundNo, "out_refund_no": req.OutRefundNo, "status": "PROCESSING"})
}

func (s *Server) wechatRefundQuery(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.wechatAuth(w, r); !ok {
		return
	}
	no := r.PathValue("no")
	s.mu.Lock()
	found := false
	for _, t := range s.trades {
		if _, ok := t.refunds[no]; ok {
			found = true
		}
	}
	s.mu.Unlock()
	if !found {
		s.wechatErr(w, http.StatusNotFound, "RESOURCE_NOT_EXISTS", "退款单不存在")
		return
	}
	s.wechatJSON(w, http.StatusOK, map[string]any{"refund_id": "50" + no, "out_refund_no": no, "status": "SUCCESS"})
}

func (s *Server) encrypt(plain []byte, aad string) map[string]string {
	nonce := randx.String(12)
	block, _ := aes.NewCipher([]byte(s.cfg.Keys.WechatAPIv3Key))
	gcm, _ := cipher.NewGCM(block)
	ct := gcm.Seal(nil, []byte(nonce), plain, []byte(aad))
	return map[string]string{"algorithm": "AEAD_AES_256_GCM", "ciphertext": base64.StdEncoding.EncodeToString(ct),
		"associated_data": aad, "nonce": nonce}
}

func (s *Server) wechatCerts(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.wechatAuth(w, r); !ok {
		return
	}
	if !s.cfg.WechatCertMode {
		s.wechatErr(w, http.StatusBadRequest, "NOT_SUPPORTED", "已切换为微信支付公钥")
		return
	}
	enc := s.encrypt([]byte(s.certPEM), "certificate")
	s.wechatJSON(w, http.StatusOK, map[string]any{"data": []map[string]any{{
		"serial_no": s.certSerial, "effective_time": time.Now().Add(-time.Hour).Format(time.RFC3339),
		"expire_time": time.Now().AddDate(5, 0, 0).Format(time.RFC3339), "encrypt_certificate": enc,
	}}})
}

func (s *Server) wechatNotify(t *trade) error {
	s.mu.Lock()
	tx := s.transaction(t)
	notifyURL := t.notifyURL
	s.mu.Unlock()
	plain, _ := json.Marshal(tx)
	res := s.encrypt(plain, "transaction")
	res["original_type"] = "transaction"
	body, _ := json.Marshal(map[string]any{
		"id": "EV-" + randx.String(20), "create_time": time.Now().In(payment.China).Format(time.RFC3339),
		"resource_type": "encrypt-resource", "event_type": "TRANSACTION.SUCCESS", "summary": "支付成功", "resource": res,
	})
	req, err := http.NewRequest(http.MethodPost, notifyURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	s.wechatSign(req.Header, body)
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("merchant replied %d %s", resp.StatusCode, b)
	}
	return nil
}

// selfSigned 为平台私钥生成自签名证书，返回十六进制大写序列号与 PEM。
func selfSigned(key *rsa.PrivateKey) (string, string) {
	if key == nil {
		return "", ""
	}
	sum := sha256.Sum256(key.N.Bytes())
	serial := new(big.Int).SetBytes(sum[:8])
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "Tenpay.com Root CA (fake)", Organization: []string{"Tenpay.com"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(5, 0, 0),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return "", ""
	}
	return strings.ToUpper(serial.Text(16)), string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}
