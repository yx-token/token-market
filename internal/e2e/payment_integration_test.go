//go:build integration

package e2e

import (
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/DueGin/token-market/internal/payment"
	"github.com/DueGin/token-market/internal/platform/config"
	"github.com/DueGin/token-market/internal/testutil/fakepay"
)

type rechargeView struct {
	ID         int64   `json:"id"`
	Status     string  `json:"status"`
	Channel    string  `json:"channel"`
	OutTradeNo *string `json:"out_trade_no"`
	Amount     float64 `json:"amount"`
	Payment    *struct {
		Kind   string `json:"kind"`
		URL    string `json:"url"`
		QRCode string `json:"qr_code"`
	} `json:"payment"`
}

// withPayments 接入支付宝与微信支付替身。
func withPayments(t *testing.T) (*fakepay.Server, func(cfg *config.Config)) {
	keys, app, merchant, err := fakepay.GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	fake := fakepay.New(fakepay.Config{Keys: keys, AlipayAppID: "2021000000000001", WechatAppID: "wx0001", WechatMchID: "1900000001",
		WechatMchSerial: "MCHSERIAL0001"})
	t.Cleanup(fake.Close)
	return fake, func(cfg *config.Config) {
		cfg.Payment.Alipay = config.AlipayConfig{AppID: "2021000000000001", Gateway: fake.AlipayGateway(),
			PrivateKey: payment.EncodePrivateKeyPEM(app), AlipayPublicKey: payment.EncodePublicKeyPEM(&keys.AlipayPlatform.PublicKey)}
		cfg.Payment.Wechat = config.WechatConfig{AppID: "wx0001", MchID: "1900000001", MchSerialNo: "MCHSERIAL0001",
			PrivateKey: payment.EncodePrivateKeyPEM(merchant), APIv3Key: keys.WechatAPIv3Key, BaseURL: fake.WechatBaseURL(),
			PublicKeyID: keys.WechatPublicKey, PublicKey: payment.EncodePublicKeyPEM(&keys.WechatPlatform.PublicKey)}
	}
}

func (e *env) settle() {
	e.t.Helper()
	e.drain()
	if err := e.app.Funds.TrackCrediting(e.ctx); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) orderStatus(id int64) string {
	var s string
	mustScan(e.t, e.app.DB.QueryRow(e.ctx, "SELECT status FROM recharge_orders WHERE id = $1", id), &s)
	return s
}

var tokenRe = regexp.MustCompile(`name="token" value="([^"]+)"`)

// browse 打开收银台 / 扫码页并点击确认付款，返回最终页面（电脑网站支付会跳回 return_url）。
func browse(t *testing.T, link string) (*http.Response, string) {
	t.Helper()
	resp, err := http.Get(link)
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	m := tokenRe.FindStringSubmatch(string(page))
	if resp.StatusCode != 200 || m == nil {
		t.Fatalf("payment page %d: %s", resp.StatusCode, page)
	}
	u, _ := url.Parse(link)
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err = noRedirect.PostForm(u.Scheme+"://"+u.Host+"/fakepay/pay", url.Values{"token": {m[1]}})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp, string(body)
}

func TestOnlinePaymentsAndOriginalRouteRefund(t *testing.T) {
	fake, opt := withPayments(t)
	e := setup(t, opt)
	admin := e.login("admin@market.test", "admin-password-1")
	admin.must("PUT", "/api/admin/settings", map[string]any{"refund_lock_seconds": 0}, nil)
	buyer := e.newUser("payer@market.test")

	var mc struct {
		Channels []string `json:"payment_channels"`
	}
	e.anon().must("GET", "/api/market/config", nil, &mc)
	if strings.Join(mc.Channels, ",") != "alipay_page,alipay_f2f,wechat_native,manual" {
		t.Fatalf("payment channels: %v", mc.Channels)
	}

	// 1. 支付宝电脑网站支付：跳转收银台 → 确认付款 → 异步通知入账 → 浏览器跳回钱包
	// 每次用新的变量解码：rechargeView 含指针字段，复用会被后续响应覆盖
	recharge := func(amount float64, channel string) rechargeView {
		var res struct {
			Order rechargeView `json:"order"`
		}
		body := map[string]any{"amount": amount}
		if channel != "" {
			body["channel"] = channel
		}
		buyer.must("POST", "/api/buyer/recharge", body, &res)
		return res.Order
	}
	page := recharge(12.34, "alipay_page")
	if page.Status != "pending" || page.Payment == nil || page.Payment.Kind != "redirect" || !strings.HasPrefix(page.Payment.URL, fake.AlipayGateway()) {
		t.Fatalf("alipay page order: %+v", page)
	}
	resp, _ := browse(t, page.Payment.URL)
	if loc := resp.Header.Get("Location"); resp.StatusCode != http.StatusFound || !strings.Contains(loc, "/buyer/wallet?order=") {
		t.Fatalf("should redirect back to wallet: %d %s", resp.StatusCode, loc)
	}
	if s := e.orderStatus(page.ID); s != "crediting" {
		t.Fatalf("alipay page order after notify: %s", s)
	}

	// 2. 支付宝当面付，通知丢失：前端轮询订单时主动查单入账
	fake.SetDropNotify(true)
	f2f := recharge(20, "alipay_f2f")
	if f2f.Payment == nil || f2f.Payment.Kind != "qrcode" || f2f.Payment.QRCode == "" {
		t.Fatalf("f2f order: %+v", f2f)
	}
	browse(t, f2f.Payment.QRCode)
	var got rechargeView
	buyer.must("GET", "/api/buyer/recharge/"+itoa(f2f.ID), nil, &got)
	if got.Status != "crediting" {
		t.Fatalf("f2f order should be credited by active query: %+v", got)
	}
	fake.SetDropNotify(false)

	// 3. 微信 Native 扫码
	wx := recharge(9.9, "wechat_native")
	if wx.Payment == nil || wx.Payment.Kind != "qrcode" {
		t.Fatalf("wechat order: %+v", wx)
	}
	browse(t, wx.Payment.QRCode)
	if s := e.orderStatus(wx.ID); s != "crediting" {
		t.Fatalf("wechat order after notify: %s", s)
	}
	// 重复通知不会重复入账
	if err := fake.Pay(*wx.OutTradeNo); err != nil {
		t.Fatal(err)
	}
	var dup int
	mustScan(t, e.app.DB.QueryRow(e.ctx, "SELECT count(*) FROM payment_notifications WHERE out_trade_no = $1 AND result = 'duplicate'", *wx.OutTradeNo), &dup)
	if dup != 1 {
		t.Fatalf("duplicate notifications recorded: %d", dup)
	}

	// 4. 用户取消后仍扫码付款（当面付二维码在超时前有效）：照常入账
	cancelled := recharge(5, "alipay_f2f")
	buyer.must("POST", "/api/buyer/recharge/"+itoa(cancelled.ID)+"/cancel", nil, nil)
	if s := e.orderStatus(cancelled.ID); s != "cancelled" {
		t.Fatalf("cancelled order: %s", s)
	}
	browse(t, cancelled.Payment.QRCode)
	if s := e.orderStatus(cancelled.ID); s != "crediting" {
		t.Fatalf("paid-after-cancel order should be credited: %s", s)
	}

	// 5. 超时未支付：worker 关单并标记过期
	expired := recharge(7, "wechat_native")
	if _, err := e.app.DB.Exec(e.ctx, `UPDATE recharge_orders SET created_at = now() - interval '1 hour', expires_at = now() - interval '5 minutes'
		WHERE id = $1`, expired.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.app.Funds.SyncPayments(e.ctx); err != nil {
		t.Fatal(err)
	}
	if s := e.orderStatus(expired.ID); s != "expired" {
		t.Fatalf("timed-out order: %s", s)
	}
	if st, _, _ := fake.TradeState(*expired.OutTradeNo); st != payment.TradeClosed {
		t.Fatalf("channel trade should be closed: %s", st)
	}

	// 6. 金额与订单不一致的通知不入账，记为计费异常
	mismatch := recharge(3, "alipay_f2f")
	result, err := e.app.Funds.ApplyTrade(e.ctx, "alipay", &payment.Trade{OutTradeNo: *mismatch.OutTradeNo, State: payment.TradePaid, AmountFen: 1})
	if err != nil || result != "amount_mismatch" || e.orderStatus(mismatch.ID) != "pending" {
		t.Fatalf("amount mismatch: %s %v %s", result, err, e.orderStatus(mismatch.ID))
	}

	// 人工充值单不能被渠道入账，在线订单也不能被管理员手工确认
	admin.expect(409, "POST", "/api/admin/recharges/"+itoa(mismatch.ID)+"/confirm", map[string]string{"provider_ref": "x"})
	manual := recharge(6, "")
	admin.must("POST", "/api/admin/recharges/"+itoa(manual.ID)+"/confirm", map[string]string{"provider_ref": "bank-001"}, nil)
	e.settle()
	for _, id := range []int64{page.ID, f2f.ID, wx.ID, cancelled.ID, manual.ID} {
		if s := e.orderStatus(id); s != "credited" {
			t.Fatalf("order %d: %s", id, s)
		}
	}
	var w struct {
		Balance *float64 `json:"balance"`
	}
	buyer.must("GET", "/api/buyer/wallet", nil, &w)
	if w.Balance == nil || !approx(*w.Balance, 12.34+20+9.9+5+6) {
		t.Fatalf("balance: %v", w.Balance)
	}

	// 7. 退款：在线充值原路退回（支付宝同步成功、微信先受理后查询成功），人工充值部分人工打款
	buyer.must("POST", "/api/buyer/refunds", map[string]string{"note": "不用了"}, nil)
	for i := 0; i < 3; i++ {
		if err := e.app.Funds.AdvanceRefunds(e.ctx); err != nil {
			t.Fatal(err)
		}
		e.drain()
	}
	var refund struct {
		ID     int64
		Status string
	}
	mustScan(t, e.app.DB.QueryRow(e.ctx, "SELECT id, status FROM refunds ORDER BY id DESC LIMIT 1"), &refund.ID, &refund.Status)
	if refund.Status != "paying" {
		t.Fatalf("refund should be paying: %s", refund.Status)
	}
	for i := 0; i < 3; i++ {
		if err := e.app.Funds.AdvanceRefundPayouts(e.ctx); err != nil {
			t.Fatal(err)
		}
		// 跳过退避等待
		if _, err := e.app.DB.Exec(e.ctx, "UPDATE refund_payouts SET next_attempt_at = now() WHERE status IN ('pending', 'processing')"); err != nil {
			t.Fatal(err)
		}
	}
	rf, err := e.app.Funds.GetRefund(e.ctx, 0, refund.ID)
	if err != nil {
		t.Fatal(err)
	}
	byChannel := map[string]float64{}
	for _, p := range rf.Payouts {
		byChannel[p.Channel+":"+p.Status] += p.Amount
	}
	if !approx(byChannel["alipay_page:success"], 12.34) || !approx(byChannel["alipay_f2f:success"], 25) ||
		!approx(byChannel["wechat_native:success"], 9.9) || !approx(byChannel["manual:manual"], 6) || rf.Status != "paying" {
		t.Fatalf("payouts: %+v status %s", byChannel, rf.Status)
	}
	for no, want := range map[string]int64{*page.OutTradeNo: 1234, *f2f.OutTradeNo: 2000, *wx.OutTradeNo: 990, *cancelled.OutTradeNo: 500} {
		if _, refunded, _ := fake.TradeState(no); refunded != want {
			t.Fatalf("channel refunded for %s: %d, want %d", no, refunded, want)
		}
	}
	admin.expect(400, "POST", "/api/admin/refunds/"+itoa(refund.ID)+"/paid", map[string]string{})
	admin.must("POST", "/api/admin/refunds/"+itoa(refund.ID)+"/paid", map[string]string{"payout_ref": "bank-refund-001"}, nil)
	if rf, err = e.app.Funds.GetRefund(e.ctx, 0, refund.ID); err != nil || rf.Status != "done" {
		t.Fatalf("refund should be done: %+v %v", rf, err)
	}
	buyer.must("GET", "/api/buyer/wallet", nil, &w)
	if w.Balance == nil || !approx(*w.Balance, 0) {
		t.Fatalf("balance after refund: %v", w.Balance)
	}

	var status struct {
		Channels []struct {
			Channel   string `json:"channel"`
			Available bool   `json:"available"`
		} `json:"channels"`
		Notifications []struct {
			Result string `json:"result"`
		} `json:"notifications"`
	}
	admin.must("GET", "/api/admin/payments/status", nil, &status)
	if len(status.Channels) != 4 || !status.Channels[0].Available || len(status.Notifications) < 4 {
		t.Fatalf("payment status: %+v", status)
	}
	// 关闭某个支付方式后买家不可再用
	admin.must("PUT", "/api/admin/settings", map[string]any{"payment_channels": map[string]bool{"wechat_native": false}}, nil)
	buyer.expect(400, "POST", "/api/buyer/recharge", map[string]any{"amount": 1, "channel": "wechat_native"})
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }
