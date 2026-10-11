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

// withPayments 启动支付宝与微信支付替身，返回在后台"支付配置"中接入它们的函数（同时检查配置接口）。
func withPayments(t *testing.T) (*fakepay.Server, func(e *env, admin *client)) {
	keys, app, merchant, err := fakepay.GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	fake := fakepay.New(fakepay.Config{Keys: keys, AlipayAppID: "2021000000000001", WechatAppID: "wx0001", WechatMchID: "1900000001",
		WechatMchSerial: "MCHSERIAL0001"})
	t.Cleanup(fake.Close)
	return fake, func(e *env, admin *client) {
		t.Helper()
		appKey := payment.EncodePrivateKeyPEM(app)
		alipayBody := map[string]any{"enabled": true, "private_key": appKey, "alipay": map[string]any{"app_id": "2021000000000001",
			"gateway": fake.AlipayGateway(), "mode": "public_key", "alipay_public_key": payment.EncodePublicKeyPEM(&keys.AlipayPlatform.PublicKey)}}
		// 启用时必须能用这份配置构建客户端：无效私钥被拒绝
		admin.expect(400, "PUT", "/api/admin/payments/merchants/alipay", map[string]any{"enabled": true, "private_key": "not a key",
			"alipay": alipayBody["alipay"]})
		admin.must("PUT", "/api/admin/payments/merchants/alipay", alipayBody, nil)
		admin.must("PUT", "/api/admin/payments/merchants/wechat", map[string]any{"enabled": true, "private_key": payment.EncodePrivateKeyPEM(merchant),
			"api_v3_key": keys.WechatAPIv3Key, "wechat": map[string]any{"app_id": "wx0001", "mch_id": "1900000001", "mch_serial_no": "MCHSERIAL0001",
				"public_key_id": keys.WechatPublicKey, "public_key": payment.EncodePublicKeyPEM(&keys.WechatPlatform.PublicKey),
				"base_url": fake.WechatBaseURL()}}, nil)
		// 没有通知地址时商户不可用
		var ms map[string]struct {
			Enabled    bool   `json:"enabled"`
			Ready      bool   `json:"ready"`
			Problem    string `json:"problem"`
			HasPrivate bool   `json:"has_private_key"`
		}
		admin.must("GET", "/api/admin/payments/merchants", nil, &ms)
		if ms["alipay"].Ready || !strings.Contains(ms["alipay"].Problem, "通知地址") {
			t.Fatalf("merchant without notify url: %+v", ms)
		}
		admin.must("PUT", "/api/admin/settings", map[string]any{"payment_public_url": e.api.URL + "/"}, nil)
		_, raw := admin.call("GET", "/api/admin/payments/merchants", nil, &ms)
		if !ms["alipay"].Ready || !ms["wechat"].Ready || !ms["alipay"].HasPrivate || strings.Contains(raw, "PRIVATE KEY") ||
			strings.Contains(raw, keys.WechatAPIv3Key) {
			t.Fatalf("merchants should be ready without exposing secrets: %s", raw)
		}
		admin.must("POST", "/api/admin/payments/merchants/alipay/test", nil, nil)
		admin.must("POST", "/api/admin/payments/merchants/wechat/test", nil, nil)
		// 只修改非敏感字段时密钥保持不变
		admin.must("PUT", "/api/admin/payments/merchants/alipay", map[string]any{"alipay": alipayBody["alipay"]}, nil)
		admin.must("POST", "/api/admin/payments/merchants/alipay/test", nil, nil)
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
	fake, configure := withPayments(t)
	e := setup(t)
	admin := e.login("admin@market.test", "admin-password-1")
	var mc0 struct {
		Channels []string `json:"payment_channels"`
	}
	e.anon().must("GET", "/api/market/config", nil, &mc0)
	if strings.Join(mc0.Channels, ",") != "manual" {
		t.Fatalf("only manual payment before merchants are configured: %v", mc0.Channels)
	}
	configure(e, admin)
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

	// 停用支付宝商户：不再创建新订单，但已创建的订单付款后照常入账
	pending := recharge(4, "alipay_f2f")
	admin.must("PUT", "/api/admin/payments/merchants/alipay", map[string]any{"enabled": false}, nil)
	buyer.expect(400, "POST", "/api/buyer/recharge", map[string]any{"amount": 1, "channel": "alipay_page"})
	browse(t, pending.Payment.QRCode)
	if s := e.orderStatus(pending.ID); s != "crediting" {
		t.Fatalf("order of a disabled merchant should still be credited: %s", s)
	}
}

type refundView struct {
	ID              int64   `json:"id"`
	Kind            string  `json:"kind"`
	RechargeOrderID *int64  `json:"recharge_order_id"`
	Status          string  `json:"status"`
	Amount          float64 `json:"amount"`
	Payouts         []struct {
		Channel         string `json:"channel"`
		Status          string `json:"status"`
		RechargeOrderID *int64 `json:"recharge_order_id"`
	} `json:"payouts"`
}

// 按充值单退款：买家或管理员对单笔订单退指定金额，不暂停 Key；扣减余额成功后才原路退回，扣减失败时打款明细作废。
func TestRechargeOrderRefund(t *testing.T) {
	fake, configure := withPayments(t)
	e := setup(t)
	admin := e.login("admin@market.test", "admin-password-1")
	configure(e, admin)
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
	manual := recharge(6, "")
	admin.must("POST", "/api/admin/recharges/"+itoa(manual.ID)+"/confirm", map[string]string{"provider_ref": "bank-002"}, nil)
	unpaid := recharge(3, "alipay_f2f")
	e.settle()
	refundPath := func(o rechargeView) string { return "/api/buyer/recharge/" + itoa(o.ID) + "/refund" }
	type wallet struct {
		Balance    *float64 `json:"balance"`
		Refundable *float64 `json:"refundable"`
	}
	balanceIs := func(want float64) {
		t.Helper()
		var w wallet
		buyer.must("GET", "/api/buyer/wallet", nil, &w)
		if w.Balance == nil || w.Refundable == nil || !approx(*w.Balance, want) || !approx(*w.Refundable, want) {
			t.Fatalf("wallet: %v %v, want %v", w.Balance, w.Refundable, want)
		}
	}
	refundedOf := func(o rechargeView) float64 {
		t.Helper()
		var got struct {
			Refunded float64 `json:"refunded_amount"`
		}
		buyer.must("GET", "/api/buyer/recharge/"+itoa(o.ID), nil, &got)
		return got.Refunded
	}
	advance := func() {
		t.Helper()
		if err := e.app.Funds.AdvanceRefunds(e.ctx); err != nil {
			t.Fatal(err)
		}
		if err := e.app.Funds.AdvanceRefundPayouts(e.ctx); err != nil {
			t.Fatal(err)
		}
	}
	balanceIs(35.9)

	// 只能退自己已到账的订单；金额不超过订单未退金额，最多两位小数
	other.expect(404, "POST", refundPath(f2f), map[string]any{"amount": 1})
	buyer.expect(409, "POST", refundPath(unpaid), map[string]any{"amount": 1})
	buyer.expect(400, "POST", refundPath(f2f), map[string]any{"amount": 20.01})
	buyer.expect(400, "POST", refundPath(f2f), map[string]any{"amount": 1.234})

	// 1. 买家退支付宝订单 5 元：不暂停 Key，扣减余额成功前不向渠道退款
	var rf refundView
	buyer.must("POST", refundPath(f2f), map[string]any{"amount": 5, "note": "退一部分"}, &rf)
	if rf.Kind != "order" || rf.Status != "subtracting" || rf.RechargeOrderID == nil || *rf.RechargeOrderID != f2f.ID ||
		len(rf.Payouts) != 1 || rf.Payouts[0].Channel != "alipay_f2f" || rf.Payouts[0].Status != "pending" {
		t.Fatalf("order refund: %+v", rf)
	}
	var locked bool
	mustScan(t, e.app.DB.QueryRow(e.ctx, "SELECT keys_locked FROM shadow_identities WHERE kind = 'buyer' AND owner_id = $1",
		userID(t, e, "order-refund@market.test")), &locked)
	if locked {
		t.Fatal("order refund should not lock keys")
	}
	buyer.expect(409, "POST", refundPath(wx), map[string]any{"amount": 1})
	buyer.expect(409, "POST", "/api/buyer/refunds", map[string]string{})
	if err := e.app.Funds.AdvanceRefundPayouts(e.ctx); err != nil {
		t.Fatal(err)
	}
	if _, refunded, _ := fake.TradeState(*f2f.OutTradeNo); refunded != 0 {
		t.Fatalf("channel refunded before balance subtraction: %d", refunded)
	}
	e.drain()
	advance()
	if err := e.app.DB.QueryRow(e.ctx, "SELECT status FROM refunds WHERE id = $1", rf.ID).Scan(&rf.Status); err != nil || rf.Status != "done" {
		t.Fatalf("order refund should be done: %s %v", rf.Status, err)
	}
	if _, refunded, _ := fake.TradeState(*f2f.OutTradeNo); refunded != 500 {
		t.Fatalf("channel refunded: %d", refunded)
	}
	if got := refundedOf(f2f); !approx(got, 5) {
		t.Fatalf("order refunded amount: %v", got)
	}
	balanceIs(30.9)
	buyer.expect(400, "POST", refundPath(f2f), map[string]any{"amount": 15.01})

	// 2. 管理员代退人工充值单：人工打款，登记流水号后完成；订单退完后不能再退
	rf = refundView{}
	admin.must("POST", "/api/admin/recharges/"+itoa(manual.ID)+"/refund", map[string]any{"amount": 6}, &rf)
	if len(rf.Payouts) != 1 || rf.Payouts[0].Channel != "manual" || rf.Payouts[0].Status != "manual" ||
		rf.Payouts[0].RechargeOrderID == nil || *rf.Payouts[0].RechargeOrderID != manual.ID {
		t.Fatalf("manual order refund: %+v", rf)
	}
	e.drain()
	advance()
	admin.must("POST", "/api/admin/refunds/"+itoa(rf.ID)+"/paid", map[string]string{"payout_ref": "bank-refund-002"}, nil)
	if r, err := e.app.Funds.GetRefund(e.ctx, 0, rf.ID); err != nil || r.Status != "done" {
		t.Fatalf("manual order refund should be done: %+v %v", r, err)
	}
	buyer.expect(400, "POST", "/api/buyer/recharge/"+itoa(manual.ID)+"/refund", map[string]any{"amount": 0.01})
	balanceIs(24.9)

	// 3. 余额不足（已被消费）时不能按订单退
	admin.must("POST", "/api/admin/adjustments", map[string]any{"buyer_user_id": userID(t, e, "order-refund@market.test"), "amount": -20,
		"reason": "模拟消费"}, nil)
	e.drain()
	balanceIs(4.9)
	buyer.expect(400, "POST", refundPath(wx), map[string]any{"amount": 5})
	if n, err := e.app.Reconciler.RunBalances(e.ctx); err != nil || n != 0 {
		t.Fatalf("balances should reconcile after order refunds: %d %v", n, err)
	}

	// 4. 扣减余额失败（期间余额被用掉）：退款失败，打款明细作废并释放订单的可退金额，不会向渠道退款
	rf = refundView{}
	buyer.must("POST", refundPath(wx), map[string]any{"amount": 4.9}, &rf)
	if got := refundedOf(wx); !approx(got, 4.9) {
		t.Fatalf("refundable amount should be held: %v", got)
	}
	if _, err := e.app.DB.Exec(e.ctx, "UPDATE sub2api_ops SET status = 'dead' WHERE id = (SELECT op_id FROM refunds WHERE id = $1)", rf.ID); err != nil {
		t.Fatal(err)
	}
	advance()
	r, err := e.app.Funds.GetRefund(e.ctx, 0, rf.ID)
	if err != nil || r.Status != "failed" || len(r.Payouts) != 1 || r.Payouts[0].Status != "cancelled" {
		t.Fatalf("failed order refund: %+v %v", r, err)
	}
	if _, refunded, _ := fake.TradeState(*wx.OutTradeNo); refunded != 0 {
		t.Fatalf("cancelled payout must not reach the channel: %d", refunded)
	}
	if got := refundedOf(wx); !approx(got, 0) {
		t.Fatalf("refundable amount should be released: %v", got)
	}
	balanceIs(4.9)
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }
