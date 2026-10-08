package alipay_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DueGin/token-market/internal/payment"
	"github.com/DueGin/token-market/internal/payment/alipay"
	"github.com/DueGin/token-market/internal/testutil/fakepay"
)

const appID = "2021000000000001"

type env struct {
	fake   *fakepay.Server
	client *alipay.Client
	mu     sync.Mutex
	got    []*payment.Trade
}

func setup(t *testing.T) *env {
	t.Helper()
	keys, app, _, err := fakepay.GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	e := &env{}
	e.fake = fakepay.New(fakepay.Config{Keys: keys, AlipayAppID: appID})
	t.Cleanup(e.fake.Close)
	var client *alipay.Client
	notify := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tr, err := client.ParseNotify(r)
		if err == nil {
			e.mu.Lock()
			e.got = append(e.got, tr)
			e.mu.Unlock()
		}
		client.AckNotify(w, err)
	}))
	t.Cleanup(notify.Close)
	client, err = alipay.New(alipay.Config{AppID: appID, Gateway: e.fake.AlipayGateway(), PrivateKey: app,
		AlipayPublicKey: &keys.AlipayPlatform.PublicKey, NotifyURL: notify.URL})
	if err != nil {
		t.Fatal(err)
	}
	e.client = client
	return e
}

func (e *env) notified() []*payment.Trade {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]*payment.Trade(nil), e.got...)
}

func order(no string, fen int64) payment.Order {
	return payment.Order{OutTradeNo: no, AmountFen: fen, Subject: "充值", ExpireAt: time.Now().Add(30 * time.Minute), ReturnURL: "http://merchant.test/return"}
}

func TestSignContent(t *testing.T) {
	v := url.Values{"b": {"2"}, "a": {"1"}, "sign": {"x"}, "empty": {""}, "sign_type": {"RSA2"}}
	if got := alipay.SignContent(v, "sign"); got != "a=1&b=2&sign_type=RSA2" {
		t.Fatalf("request content: %q", got)
	}
	if got := alipay.SignContent(v, "sign", "sign_type"); got != "a=1&b=2" {
		t.Fatalf("notify content: %q", got)
	}
}

func TestPagePayAndNotify(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	in, err := e.client.Create(ctx, payment.ChannelAlipayPage, order("TMR1abc", 1234))
	if err != nil {
		t.Fatal(err)
	}
	if in.Kind != "redirect" || !strings.HasPrefix(in.URL, e.fake.AlipayGateway()+"?") {
		t.Fatalf("intent: %+v", in)
	}
	resp, err := http.Get(in.URL)
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(page), "确认付款") || !strings.Contains(string(page), "12.34") {
		t.Fatalf("cashier page %d: %s", resp.StatusCode, page)
	}
	tr, err := e.client.Query(ctx, "TMR1abc")
	if err != nil || tr.State != payment.TradePending {
		t.Fatalf("query before pay: %+v %v", tr, err)
	}
	if err := e.fake.Pay("TMR1abc"); err != nil {
		t.Fatal(err)
	}
	got := e.notified()
	if len(got) != 1 || got[0].OutTradeNo != "TMR1abc" || got[0].State != payment.TradePaid || got[0].AmountFen != 1234 || got[0].TradeNo == "" {
		t.Fatalf("notify: %+v", got)
	}
	tr, err = e.client.Query(ctx, "TMR1abc")
	if err != nil || tr.State != payment.TradePaid || tr.AmountFen != 1234 || tr.PaidAt == nil {
		t.Fatalf("query after pay: %+v %v", tr, err)
	}
	if err := e.client.Close(ctx, "TMR1abc"); err == nil {
		t.Fatal("closing a paid trade should fail")
	}
}

func TestPrecreateCloseAndRefund(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	in, err := e.client.Create(ctx, payment.ChannelAlipayF2F, order("TMR2abc", 5000))
	if err != nil {
		t.Fatal(err)
	}
	if in.Kind != "qrcode" || in.QRCode == "" {
		t.Fatalf("intent: %+v", in)
	}
	// 用户扫码前支付宝侧没有交易
	if _, err := e.client.Query(ctx, "TMR2abc"); !errors.Is(err, payment.ErrTradeNotFound) {
		t.Fatalf("query before scan: %v", err)
	}
	if err := e.client.Close(ctx, "TMR2abc"); err != nil {
		t.Fatalf("close unscanned: %v", err)
	}
	if err := e.fake.Pay("TMR2abc"); err != nil {
		t.Fatal(err)
	}
	if len(e.notified()) != 1 {
		t.Fatal("expected notify")
	}
	r, err := e.client.Refund(ctx, payment.Refund{OutTradeNo: "TMR2abc", OutRefundNo: "RF1", AmountFen: 2000, TotalFen: 5000, Reason: "余额退款"})
	if err != nil || r.State != payment.RefundSuccess {
		t.Fatalf("refund: %+v %v", r, err)
	}
	// 同一退款单号重复提交是幂等的：fund_change = N，以查询为准
	r, err = e.client.Refund(ctx, payment.Refund{OutTradeNo: "TMR2abc", OutRefundNo: "RF1", AmountFen: 2000, TotalFen: 5000})
	if err != nil || r.State != payment.RefundProcessing {
		t.Fatalf("duplicate refund: %+v %v", r, err)
	}
	q, err := e.client.QueryRefund(ctx, "TMR2abc", "RF1")
	if err != nil || q.State != payment.RefundSuccess {
		t.Fatalf("refund query: %+v %v", q, err)
	}
	if _, err := e.client.QueryRefund(ctx, "TMR2abc", "RF-unknown"); !errors.Is(err, payment.ErrRefundNotFound) {
		t.Fatalf("unknown refund: %v", err)
	}
	r, err = e.client.Refund(ctx, payment.Refund{OutTradeNo: "TMR2abc", OutRefundNo: "RF2", AmountFen: 4000, TotalFen: 5000})
	if err != nil || r.State != payment.RefundFailed {
		t.Fatalf("over-refund should fail: %+v %v", r, err)
	}
	if _, refunded, _ := e.fake.TradeState("TMR2abc"); refunded != 2000 {
		t.Fatalf("refunded = %d", refunded)
	}
}

func TestRejectsBadSignatures(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	bad, err := alipay.New(alipay.Config{AppID: appID, Gateway: e.fake.AlipayGateway(), PrivateKey: other,
		AlipayPublicKey: &other.PublicKey, NotifyURL: "http://x"})
	if err != nil {
		t.Fatal(err)
	}
	// 请求签名错误：网关拒绝
	if _, err := bad.Create(ctx, payment.ChannelAlipayF2F, order("TMR3abc", 100)); err == nil || !strings.Contains(err.Error(), "invalid-signature") {
		t.Fatalf("expected signature rejection, got %v", err)
	}
	// 伪造的通知：验签失败
	form := url.Values{"app_id": {appID}, "out_trade_no": {"TMR3abc"}, "trade_status": {"TRADE_SUCCESS"}, "total_amount": {"1.00"}, "sign_type": {"RSA2"}}
	sig, _ := payment.SignSHA256(other, alipay.SignContent(form, "sign", "sign_type"))
	form.Set("sign", sig)
	req := httptest.NewRequest(http.MethodPost, "/notify", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if _, err := e.client.ParseNotify(req); err == nil {
		t.Fatal("forged notify accepted")
	}
}

func TestCertSN(t *testing.T) {
	mk := func(cn string, serial int64) (*x509.Certificate, string) {
		key, _ := rsa.GenerateKey(rand.Reader, 2048)
		tmpl := &x509.Certificate{SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: cn, Organization: []string{"Ant"}},
			NotBefore: time.Now(), NotAfter: time.Now().Add(time.Hour)}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
		if err != nil {
			t.Fatal(err)
		}
		c, _ := x509.ParseCertificate(der)
		return c, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	}
	c1, p1 := mk("root1", 1001)
	c2, p2 := mk("root2", 1002)
	sn := alipay.CertSN(c1)
	if len(sn) != 32 || sn == alipay.CertSN(c2) {
		t.Fatalf("cert sn %q", sn)
	}
	root, err := alipay.RootCertSN(p1 + p2)
	if err != nil || root != alipay.CertSN(c1)+"_"+alipay.CertSN(c2) {
		t.Fatalf("root sn %q %v", root, err)
	}
}
