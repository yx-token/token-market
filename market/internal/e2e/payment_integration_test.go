//go:build integration

package e2e

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yx-token/token-market/market/hostpay"
	"github.com/yx-token/token-market/market/internal/payment"
	"github.com/yx-token/token-market/market/internal/platform/config"
	"github.com/yx-token/token-market/market/internal/testutil/fakepay"
)

type rechargeView struct {
	ID                 int64      `json:"id"`
	Status             string     `json:"status"`
	Channel            string     `json:"channel"`
	ProviderInstanceID *int64     `json:"provider_instance_id"`
	OutTradeNo         *string    `json:"out_trade_no"`
	Amount             float64    `json:"amount"`
	ExpiresAt          *time.Time `json:"expires_at"`
	Payment            *struct {
		Kind   string `json:"kind"`
		URL    string `json:"url"`
		QRCode string `json:"qr_code"`
	} `json:"payment"`
}

type paymentFixture struct {
	fake   *fakepay.Server
	config hostpay.Config
}

// withPayments 模拟宿主提供的支付实例；测试网关仅通过进程配置覆盖。
func withPayments(t *testing.T) *paymentFixture {
	t.Helper()
	hostpay.SetSource(nil)
	t.Cleanup(func() { hostpay.SetSource(nil) })
	keys, app, merchant, err := fakepay.GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	fake := fakepay.New(fakepay.Config{Keys: keys, AlipayAppID: "2021000000000001", WechatAppID: "wx0001", WechatMchID: "1900000001",
		WechatMchSerial: "MCHSERIAL0001"})
	t.Cleanup(fake.Close)
	return &paymentFixture{fake: fake, config: hostpay.Config{Enabled: true, Instances: []hostpay.Instance{
		{ID: 101, ProviderKey: hostpay.ProviderAlipay, Enabled: true, SupportedTypes: "alipay", Config: map[string]string{
			"appId": "2021000000000001", "privateKey": payment.EncodePrivateKeyPEM(app),
			"publicKey": payment.EncodePublicKeyPEM(&keys.AlipayPlatform.PublicKey),
		}},
		{ID: 201, ProviderKey: hostpay.ProviderWxpay, Enabled: true, SupportedTypes: "wxpay", Config: map[string]string{
			"appId": "wx0001", "mchId": "1900000001", "certSerial": "MCHSERIAL0001",
			"privateKey": payment.EncodePrivateKeyPEM(merchant), "apiV3Key": keys.WechatAPIv3Key,
			"publicKeyId": keys.WechatPublicKey, "publicKey": payment.EncodePublicKeyPEM(&keys.WechatPlatform.PublicKey),
		}},
	}}}
}

func (p *paymentFixture) gateways(cfg *config.Config) {
	cfg.Payment.AlipayGateway = p.fake.AlipayGateway()
	cfg.Payment.WechatBaseURL = p.fake.WechatBaseURL()
}

func (p *paymentFixture) configure(e *env) {
	for i := range p.config.Instances {
		p.config.Instances[i].Config["notifyUrl"] = e.api.URL
	}
	p.publish(e)
}

func (p *paymentFixture) publish(e *env) {
	// 每次发布独立快照，避免 HTTP 请求读取配置时与下一次测试修改共享 map。
	snapshot := p.config
	snapshot.Instances = append([]hostpay.Instance(nil), p.config.Instances...)
	for i, instance := range snapshot.Instances {
		snapshot.Instances[i].Config = make(map[string]string, len(instance.Config))
		for k, v := range instance.Config {
			snapshot.Instances[i].Config[k] = v
		}
	}
	hostpay.SetSource(func(context.Context) (*hostpay.Config, error) { return &snapshot, nil })
	e.app.Payments.Invalidate()
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

func TestRechargeRequiresHostPaymentSourceAndChannel(t *testing.T) {
	hostpay.SetSource(nil)
	t.Cleanup(func() { hostpay.SetSource(nil) })
	e := setup(t)
	buyer := e.newUser("no-payment-source@market.test")
	if raw := buyer.expect(400, "POST", "/api/buyer/recharge", map[string]any{"amount": 1}); !strings.Contains(raw, "CHANNEL_REQUIRED") {
		t.Fatalf("missing channel should be required: %s", raw)
	}
	if raw := buyer.expect(400, "POST", "/api/buyer/recharge", map[string]any{"amount": 1, "channel": payment.ChannelManual}); !strings.Contains(raw, "CHANNEL_UNAVAILABLE") {
		t.Fatalf("manual channel should be unavailable: %s", raw)
	}
}

func TestPaymentNotifyRejectsForgedAlipaySignatureOnBothRoutes(t *testing.T) {
	p := withPayments(t)
	e := setup(t, p.gateways)
	p.configure(e)
	buyer := e.newUser("forged-notify@market.test")
	var created struct {
		Order rechargeView `json:"order"`
	}
	buyer.must("POST", "/api/buyer/recharge", map[string]any{"amount": 5, "channel": "alipay_f2f"}, &created)
	if created.Order.Status != "pending" {
		t.Fatalf("initial order status = %q, want pending", created.Order.Status)
	}

	for _, route := range []string{
		"/api/payments/alipay/101/notify",
		"/api/payments/alipay/notify",
	} {
		req, err := http.NewRequest(http.MethodPost, e.api.URL+route, strings.NewReader("app_id=2021000000000001&out_trade_no=forged&sign=invalid"))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != "fail" {
			t.Fatalf("forged notification %s status/body = %d, want 200 fail", route, resp.StatusCode)
		}
		if got := e.orderStatus(created.Order.ID); got != "pending" {
			t.Fatalf("forged notification %s changed order status to %q", route, got)
		}
	}
	var invalid int
	mustScan(t, e.app.DB.QueryRow(e.ctx, `SELECT count(*) FROM payment_notifications WHERE provider = 'alipay' AND result = 'invalid'
		AND error LIKE '%signature%'`), &invalid)
	if invalid != 2 {
		t.Fatalf("invalid forged notification records = %d, want 2", invalid)
	}
}

func TestInFlightPaymentOrderCounterIncludesOrdersAndOriginalRoutePayouts(t *testing.T) {
	p := withPayments(t)
	e := setup(t, p.gateways)
	p.configure(e)
	buyer := e.newUser("inflight-counter@market.test")
	var created struct {
		Order rechargeView `json:"order"`
	}
	buyer.must("POST", "/api/buyer/recharge", map[string]any{"amount": 5, "channel": "alipay_f2f"}, &created)

	count, err := e.app.Funds.InFlightPaymentOrders(e.ctx, 101)
	if err != nil || count != 1 {
		t.Fatalf("pending order in-flight count = %d, %v; want 1", count, err)
	}
	if _, err := e.app.DB.Exec(e.ctx, `UPDATE recharge_orders SET status = 'cancelled', created_at = now() - interval '1 hour',
		expires_at = now() + interval '5 minutes', last_synced_at = NULL WHERE id = $1`, created.Order.ID); err != nil {
		t.Fatal(err)
	}
	count, err = e.app.Funds.InFlightPaymentOrders(e.ctx, 101)
	if err != nil || count != 1 {
		t.Fatalf("eligible cancelled order in-flight count = %d, %v; want 1", count, err)
	}
	// 查单节流只影响 worker 何时再查，不影响"仍依赖原凭证"：刚查过单的已取消订单也要计入
	if _, err := e.app.DB.Exec(e.ctx, `UPDATE recharge_orders SET last_synced_at = now() WHERE id = $1`, created.Order.ID); err != nil {
		t.Fatal(err)
	}
	count, err = e.app.Funds.InFlightPaymentOrders(e.ctx, 101)
	if err != nil || count != 1 {
		t.Fatalf("recently synced cancelled order in-flight count = %d, %v; want 1", count, err)
	}
	// 超出查单窗口（支付时限后 10 分钟）的已取消订单不再计入
	if _, err := e.app.DB.Exec(e.ctx, `UPDATE recharge_orders SET expires_at = now() - interval '11 minutes' WHERE id = $1`, created.Order.ID); err != nil {
		t.Fatal(err)
	}
	count, err = e.app.Funds.InFlightPaymentOrders(e.ctx, 101)
	if err != nil || count != 0 {
		t.Fatalf("cancelled order past polling window in-flight count = %d, %v; want 0", count, err)
	}
	if other, err := e.app.Funds.InFlightPaymentOrders(e.ctx, 999); err != nil || other != 0 {
		t.Fatalf("unrelated instance in-flight count = %d, %v; want 0", other, err)
	}

	var userID int64
	mustScan(t, e.app.DB.QueryRow(e.ctx, "SELECT user_id FROM recharge_orders WHERE id = $1", created.Order.ID), &userID)
	var refundID int64
	if err := e.app.DB.QueryRow(e.ctx, `INSERT INTO refunds (user_id, kind, status, amount, lock_until)
		VALUES ($1, 'order', 'paying', 1, now()) RETURNING id`, userID).Scan(&refundID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.app.DB.Exec(e.ctx, `INSERT INTO refund_payouts (refund_id, recharge_order_id, channel, amount, out_refund_no, status)
		VALUES ($1, $2, 'alipay_f2f', 1, 'inflight-counter-refund', 'pending')`, refundID, created.Order.ID); err != nil {
		t.Fatal(err)
	}
	count, err = e.app.Funds.InFlightPaymentOrders(e.ctx, 101)
	if err != nil || count != 1 {
		t.Fatalf("payout-only in-flight count = %d, %v; want 1", count, err)
	}
}

func TestOnlinePaymentsAndOriginalRouteRefund(t *testing.T) {
	p := withPayments(t)
	e := setup(t, p.gateways)
	admin := e.login("admin@market.test", "admin-password-1")
	var mc0 struct {
		Channels []string `json:"payment_channels"`
	}
	e.anon().must("GET", "/api/market/config", nil, &mc0)
	if len(mc0.Channels) != 0 {
		t.Fatalf("payment channels must be empty without host payment source: %v", mc0.Channels)
	}
	p.configure(e)
	buyerForRejectedRequests := e.newUser("rejected-payment@market.test")
	if raw := buyerForRejectedRequests.expect(400, "POST", "/api/buyer/recharge", map[string]any{"amount": 1}); !strings.Contains(raw, "CHANNEL_REQUIRED") {
		t.Fatalf("missing channel must be rejected: %s", raw)
	}
	if raw := buyerForRejectedRequests.expect(400, "POST", "/api/buyer/recharge", map[string]any{"amount": 1, "channel": payment.ChannelManual}); !strings.Contains(raw, "CHANNEL_UNAVAILABLE") {
		t.Fatalf("manual channel must be rejected: %s", raw)
	}
	admin.must("PUT", "/api/admin/settings", map[string]any{"refund_lock_seconds": 0}, nil)
	buyer := e.newUser("payer@market.test")

	var mc struct {
		Channels []string `json:"payment_channels"`
	}
	e.anon().must("GET", "/api/market/config", nil, &mc)
	if strings.Join(mc.Channels, ",") != "alipay_page,alipay_f2f,wechat_native" {
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
	if page.Status != "pending" || page.ProviderInstanceID == nil || *page.ProviderInstanceID != 101 || page.Payment == nil || page.Payment.Kind != "redirect" || !strings.HasPrefix(page.Payment.URL, p.fake.AlipayGateway()) {
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
	p.fake.SetDropNotify(true)
	f2f := recharge(20, "alipay_f2f")
	if f2f.ProviderInstanceID == nil || *f2f.ProviderInstanceID != 101 || f2f.Payment == nil || f2f.Payment.Kind != "qrcode" || f2f.Payment.QRCode == "" {
		t.Fatalf("f2f order: %+v", f2f)
	}
	browse(t, f2f.Payment.QRCode)
	var got rechargeView
	buyer.must("GET", "/api/buyer/recharge/"+itoa(f2f.ID), nil, &got)
	if got.Status != "crediting" {
		t.Fatalf("f2f order should be credited by active query: %+v", got)
	}
	p.fake.SetDropNotify(false)

	// 3. 微信 Native 扫码
	wx := recharge(9.9, "wechat_native")
	if wx.ProviderInstanceID == nil || *wx.ProviderInstanceID != 201 || wx.Payment == nil || wx.Payment.Kind != "qrcode" {
		t.Fatalf("wechat order: %+v", wx)
	}
	browse(t, wx.Payment.QRCode)
	if s := e.orderStatus(wx.ID); s != "crediting" {
		t.Fatalf("wechat order after notify: %s", s)
	}
	// 重复通知不会重复入账
	if err := p.fake.Pay(*wx.OutTradeNo); err != nil {
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
	if st, _, _ := p.fake.TradeState(*expired.OutTradeNo); st != payment.TradeClosed {
		t.Fatalf("channel trade should be closed: %s", st)
	}

	// 6. 金额与订单不一致的通知不入账，记为计费异常
	mismatch := recharge(3, "alipay_f2f")
	if _, err := e.app.DB.Exec(e.ctx, "UPDATE recharge_orders SET provider_instance_id = NULL WHERE id = $1", mismatch.ID); err != nil {
		t.Fatal(err)
	}
	result, err := e.app.Funds.ApplyTrade(e.ctx, "alipay", &payment.Trade{OutTradeNo: *mismatch.OutTradeNo, State: payment.TradePaid, AmountFen: 300}, 101)
	if err != nil || result != "ignored" || e.orderStatus(mismatch.ID) != "pending" {
		t.Fatalf("instance callback must not claim a legacy order: %s %v %s", result, err, e.orderStatus(mismatch.ID))
	}
	result, err = e.app.Funds.ApplyTrade(e.ctx, "alipay", &payment.Trade{OutTradeNo: *mismatch.OutTradeNo, State: payment.TradePaid, AmountFen: 1}, 0)
	if err != nil || result != "amount_mismatch" || e.orderStatus(mismatch.ID) != "pending" {
		t.Fatalf("legacy callback amount mismatch: %s %v %s", result, err, e.orderStatus(mismatch.ID))
	}

	// 人工充值单不能被渠道入账，在线订单也不能被管理员手工确认
	admin.expect(409, "POST", "/api/admin/recharges/"+itoa(mismatch.ID)+"/confirm", map[string]string{"provider_ref": "x"})
	manual := historicalManual(t, e, buyer, 6)
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

	// 7. 买家侧不可退款：余额退款与按单退款入口均拒绝，渠道未发生退款，余额保持入账结果
	credited := 12.34 + 20 + 9.9 + 5 + 6
	if raw := buyer.expect(403, "POST", "/api/buyer/refunds", map[string]string{"note": "不用了"}); !strings.Contains(raw, "BUYER_REFUNDS_DISABLED") {
		t.Fatalf("buyer wallet refund must be disabled: %s", raw)
	}
	if raw := buyer.expect(403, "POST", "/api/buyer/recharge/"+itoa(f2f.ID)+"/refund", map[string]any{"amount": 1}); !strings.Contains(raw, "BUYER_REFUNDS_DISABLED") {
		t.Fatalf("buyer order refund must be disabled: %s", raw)
	}
	if raw := admin.expect(403, "POST", "/api/admin/recharges/"+itoa(manual.ID)+"/refund", map[string]any{"amount": 1}); !strings.Contains(raw, "BUYER_REFUNDS_DISABLED") {
		t.Fatalf("admin-on-behalf order refund must be disabled: %s", raw)
	}
	var refundRows int
	mustScan(t, e.app.DB.QueryRow(e.ctx, "SELECT count(*) FROM refunds"), &refundRows)
	if refundRows != 0 {
		t.Fatalf("disabled refund must not create rows: %d", refundRows)
	}
	for no := range map[string]struct{}{*page.OutTradeNo: {}, *f2f.OutTradeNo: {}, *wx.OutTradeNo: {}, *cancelled.OutTradeNo: {}} {
		if _, refunded, _ := p.fake.TradeState(no); refunded != 0 {
			t.Fatalf("channel must not refund when buyer refunds disabled: %s refunded=%d", no, refunded)
		}
	}
	buyer.must("GET", "/api/buyer/wallet", nil, &w)
	if w.Balance == nil || !approx(*w.Balance, credited) {
		t.Fatalf("balance after denied refund: %v want %v", w.Balance, credited)
	}

	// 停用支付宝实例：不再创建新订单，但已创建的订单付款后照常入账
	pending := recharge(4, "alipay_f2f")
	p.config.Instances[0].Enabled = false
	p.publish(e)
	buyer.expect(400, "POST", "/api/buyer/recharge", map[string]any{"amount": 1, "channel": "alipay_page"})
	browse(t, pending.Payment.QRCode)
	if s := e.orderStatus(pending.ID); s != "crediting" {
		t.Fatalf("order of a disabled merchant should still be credited: %s", s)
	}
}

// 按充值单退款入口：产品政策下买家与管理员代退均不可用。
func TestRechargeOrderRefund(t *testing.T) {
	p := withPayments(t)
	e := setup(t, p.gateways)
	admin := e.login("admin@market.test", "admin-password-1")
	p.configure(e)
	buyer := e.newUser("order-refund@market.test")
	other := e.newUser("other-buyer@market.test")
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
	f2f := recharge(20, "alipay_f2f")
	browse(t, f2f.Payment.QRCode)
	wx := recharge(9.9, "wechat_native")
	browse(t, wx.Payment.QRCode)
	manual := historicalManual(t, e, buyer, 6)
	admin.must("POST", "/api/admin/recharges/"+itoa(manual.ID)+"/confirm", map[string]string{"provider_ref": "bank-002"}, nil)
	e.settle()
	refundPath := func(o rechargeView) string { return "/api/buyer/recharge/" + itoa(o.ID) + "/refund" }
	var w struct {
		Balance    *float64 `json:"balance"`
		Refundable *float64 `json:"refundable"`
	}
	buyer.must("GET", "/api/buyer/wallet", nil, &w)
	if w.Balance == nil || !approx(*w.Balance, 35.9) {
		t.Fatalf("wallet before denied refund: %+v", w)
	}
	for _, tc := range []struct {
		who  *client
		path string
		body any
	}{
		{other, refundPath(f2f), map[string]any{"amount": 1}},
		{buyer, refundPath(f2f), map[string]any{"amount": 5}},
		{buyer, refundPath(wx), map[string]any{"amount": 1}},
		{buyer, "/api/buyer/refunds", map[string]string{}},
		{admin, "/api/admin/recharges/" + itoa(manual.ID) + "/refund", map[string]any{"amount": 6}},
	} {
		if raw := tc.who.expect(403, "POST", tc.path, tc.body); !strings.Contains(raw, "BUYER_REFUNDS_DISABLED") {
			t.Fatalf("refund must be disabled for %s: %s", tc.path, raw)
		}
	}
	var refundRows int
	mustScan(t, e.app.DB.QueryRow(e.ctx, "SELECT count(*) FROM refunds"), &refundRows)
	if refundRows != 0 {
		t.Fatalf("disabled refund must not create rows: %d", refundRows)
	}
	for _, no := range []string{*f2f.OutTradeNo, *wx.OutTradeNo} {
		if _, refunded, _ := p.fake.TradeState(no); refunded != 0 {
			t.Fatalf("channel must not refund: %s refunded=%d", no, refunded)
		}
	}
	buyer.must("GET", "/api/buyer/wallet", nil, &w)
	if w.Balance == nil || !approx(*w.Balance, 35.9) {
		t.Fatalf("wallet after denied refund: %+v", w)
	}
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

func historicalManual(t *testing.T, e *env, buyer *client, amount float64) rechargeView {
	t.Helper()
	var me struct {
		ID int64 `json:"id"`
	}
	buyer.must("GET", "/api/auth/me", nil, &me)
	var order rechargeView
	err := e.app.DB.QueryRow(e.ctx, `INSERT INTO recharge_orders (user_id, amount, provider, channel)
		VALUES ($1, $2::numeric, 'manual', 'manual') RETURNING id, status, channel, amount::float8`, me.ID, amount).
		Scan(&order.ID, &order.Status, &order.Channel, &order.Amount)
	if err != nil {
		t.Fatal(err)
	}
	return order
}
