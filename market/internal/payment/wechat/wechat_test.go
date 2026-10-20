package wechat_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yx-token/token-market/market/internal/payment"
	"github.com/yx-token/token-market/market/internal/payment/wechat"
	"github.com/yx-token/token-market/market/internal/testutil/fakepay"
)

type env struct {
	fake   *fakepay.Server
	client *wechat.Client
	mu     sync.Mutex
	got    []*payment.Trade
	errs   []error
}

func setup(t *testing.T, certMode bool) *env {
	t.Helper()
	keys, _, merchant, err := fakepay.GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	e := &env{}
	e.fake = fakepay.New(fakepay.Config{Keys: keys, WechatAppID: "wx0001", WechatMchID: "1900000001",
		WechatMchSerial: "MCHSERIAL0001", WechatCertMode: certMode})
	t.Cleanup(e.fake.Close)
	var client *wechat.Client
	notify := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tr, err := client.ParseNotify(r)
		e.mu.Lock()
		if err != nil {
			e.errs = append(e.errs, err)
		} else if tr != nil {
			e.got = append(e.got, tr)
		}
		e.mu.Unlock()
		client.AckNotify(w, err)
	}))
	t.Cleanup(notify.Close)
	cfg := wechat.Config{AppID: "wx0001", MchID: "1900000001", MchSerialNo: "MCHSERIAL0001", PrivateKey: merchant,
		APIv3Key: keys.WechatAPIv3Key, BaseURL: e.fake.WechatBaseURL(), NotifyURL: notify.URL}
	if !certMode {
		cfg.PublicKeyID, cfg.PublicKey = keys.WechatPublicKey, &keys.WechatPlatform.PublicKey
	}
	if client, err = wechat.New(cfg); err != nil {
		t.Fatal(err)
	}
	e.client = client
	return e
}

func run(t *testing.T, certMode bool) {
	e := setup(t, certMode)
	ctx := context.Background()
	in, err := e.client.Create(ctx, payment.ChannelWechatNative, payment.Order{OutTradeNo: "TMR9wx", AmountFen: 990, Subject: "充值",
		ExpireAt: time.Now().Add(30 * time.Minute), ClientIP: "203.0.113.9"})
	if err != nil {
		t.Fatal(err)
	}
	if in.Kind != "qrcode" || in.QRCode == "" {
		t.Fatalf("intent %+v", in)
	}
	tr, err := e.client.Query(ctx, "TMR9wx")
	if err != nil || tr.State != payment.TradePending {
		t.Fatalf("query before pay: %+v %v", tr, err)
	}
	if _, err := e.client.Query(ctx, "TMR-missing"); !errors.Is(err, payment.ErrTradeNotFound) {
		t.Fatalf("missing trade: %v", err)
	}
	if err := e.fake.Pay("TMR9wx"); err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	got, errs := e.got, e.errs
	e.mu.Unlock()
	if len(errs) > 0 || len(got) != 1 || got[0].State != payment.TradePaid || got[0].AmountFen != 990 || got[0].TradeNo == "" || got[0].PaidAt == nil {
		t.Fatalf("notify: %+v errs %v", got, errs)
	}
	if err := e.client.Close(ctx, "TMR9wx"); err == nil {
		t.Fatal("closing a paid order should fail")
	}
	r, err := e.client.Refund(ctx, payment.Refund{OutTradeNo: "TMR9wx", OutRefundNo: "RFW1", AmountFen: 500, TotalFen: 990, Reason: "余额退款"})
	if err != nil || r.State != payment.RefundProcessing {
		t.Fatalf("refund: %+v %v", r, err)
	}
	q, err := e.client.QueryRefund(ctx, "TMR9wx", "RFW1")
	if err != nil || q.State != payment.RefundSuccess {
		t.Fatalf("refund query: %+v %v", q, err)
	}
	r, err = e.client.Refund(ctx, payment.Refund{OutTradeNo: "TMR9wx", OutRefundNo: "RFW2", AmountFen: 600, TotalFen: 990})
	if err != nil || r.State != payment.RefundFailed {
		t.Fatalf("over-refund: %+v %v", r, err)
	}
	tr, err = e.client.Query(ctx, "TMR9wx")
	if err != nil || tr.State != payment.TradePaid {
		t.Fatalf("query after refund: %+v %v", tr, err)
	}
}

func TestNativeWithPublicKey(t *testing.T) { run(t, false) }

func TestNativeWithPlatformCertificates(t *testing.T) { run(t, true) }

func TestNotifyRejectsTampering(t *testing.T) {
	e := setup(t, false)
	body := []byte(`{"id":"EV-1","event_type":"TRANSACTION.SUCCESS","resource":{}}`)
	req := httptest.NewRequest(http.MethodPost, "/notify", bytes.NewReader(body))
	req.Header.Set("Wechatpay-Serial", "PUB_KEY_ID_FAKE0001")
	req.Header.Set("Wechatpay-Timestamp", "1700000000")
	req.Header.Set("Wechatpay-Nonce", "abc")
	req.Header.Set("Wechatpay-Signature", "c2lnbmF0dXJl")
	if _, err := e.client.ParseNotify(req); err == nil || !strings.Contains(err.Error(), "verification") {
		t.Fatalf("forged notify: %v", err)
	}
}
