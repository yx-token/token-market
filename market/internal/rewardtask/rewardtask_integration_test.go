//go:build integration

package rewardtask

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yx-token/token-market/market/internal/platform/config"
	"github.com/yx-token/token-market/market/internal/platform/crypto"
	"github.com/yx-token/token-market/market/internal/testutil"
)

func TestMain(m *testing.M) {
	code := m.Run()
	if err := testutil.CleanupTemplates(); err != nil {
		fmt.Fprintf(os.Stderr, "cleanup reward task database templates: %v\n", err)
		code = 1
	}
	os.Exit(code)
}

const (
	testSecret = "rs_integration_secret_0123456789"
	ticketA    = "rt_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	ticketB    = "rt_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	ticketC    = "rt_cccccccccccccccccccccccccccccccccccccccc"
)

// fakeLoxo 按 loxo 的规则验签，票据查询结果与事件响应由测试预置。
type fakeLoxo struct {
	t       *testing.T
	mu      sync.Mutex
	tickets map[string]TicketView
	// eventReply 返回 (HTTP 状态, reason, result)；reason 为空表示成功。
	eventReply func(in map[string]any) (int, string, string)
	events     []map[string]any
	queries    int
}

func (f *fakeLoxo) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	verifyLikeLoxo(f.t, r, testSecret, body, time.Now())
	f.mu.Lock()
	defer f.mu.Unlock()
	fail := func(status int, reason string) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": status, "message": "rejected", "reason": reason})
	}
	ok := func(data any) {
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "message": "success", "data": data})
	}
	switch r.URL.Path {
	case "/api/v1/reward-open/ping":
		ok(map[string]any{"partner_name": "闲猫", "app_key": r.Header.Get("X-Reward-App-Key"), "server_time": time.Now().Unix()})
	case "/api/v1/reward-open/tickets/query":
		f.queries++
		var in struct{ Ticket string }
		_ = json.Unmarshal(body, &in)
		v, found := f.tickets[in.Ticket]
		if !found {
			fail(404, "REWARD_TICKET_NOT_FOUND")
			return
		}
		ok(v)
	case "/api/v1/reward-open/events":
		var in map[string]any
		_ = json.Unmarshal(body, &in)
		f.events = append(f.events, in)
		status, reason, result := 200, "", "accepted"
		if f.eventReply != nil {
			status, reason, result = f.eventReply(in)
		}
		if reason != "" {
			fail(status, reason)
			return
		}
		taskStatus := "in_progress"
		if result == "completed" || result == "ignored" {
			taskStatus = "completed"
		}
		ok(map[string]any{"event_id": in["event_id"], "result": result, "status": taskStatus, "rewarded": result == "completed"})
	default:
		fail(404, "")
	}
}

func (f *fakeLoxo) sent() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any(nil), f.events...)
}

type env struct {
	s    *Service
	pool *pgxpool.Pool
	loxo *fakeLoxo
	now  time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()
	_, pool := testutil.NewMarketDatabase(t)
	kr, err := crypto.NewKeyring(config.CryptoConfig{ActiveKeyID: "k1", Keys: map[string]string{"k1": base64.StdEncoding.EncodeToString([]byte(strings.Repeat("r", 32)))}})
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeLoxo{t: t, tickets: map[string]TicketView{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	// 测试时钟比数据库 now() 略快，保证刚插入的行（next_check_at 默认 now()）在本轮就到期。
	e := &env{pool: pool, loxo: f, now: time.Now().Add(2 * time.Second)}
	e.s = &Service{DB: pool, Keyring: kr, Now: func() time.Time { return e.now }}
	// httptest 地址是 http://127.0.0.1:port，正好在本地联调白名单里。
	if err := e.s.SaveConfig(context.Background(), ConfigInput{Enabled: true, BaseURL: srv.URL, AppKey: "rk_test", AppSecret: testSecret, SDKEnabled: true}); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *env) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := e.pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("fixture: %v", err)
	}
}

func (e *env) user(t *testing.T, email string) int64 {
	t.Helper()
	var id int64
	if err := e.pool.QueryRow(context.Background(), `INSERT INTO users(email,password_hash) VALUES($1,'x') RETURNING id`, email).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func (e *env) recharge(t *testing.T, userID int64, amount string, status string, paidAt time.Time) int64 {
	t.Helper()
	var id int64
	// 下单时间取 paidAt 前 1 秒，支付时间按渠道习惯截到整秒。
	if err := e.pool.QueryRow(context.Background(), `INSERT INTO recharge_orders(user_id,amount,provider,status,created_at,paid_at) VALUES($1,$2,'alipay',$3,$4,date_trunc('second',$5::timestamptz)) RETURNING id`,
		userID, amount, status, paidAt.Add(-time.Second), paidAt).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func (e *env) ticketState(t *testing.T, ticket string) (string, string) {
	t.Helper()
	var state, reason string
	if err := e.pool.QueryRow(context.Background(), `SELECT state, reason FROM reward_tickets WHERE ticket=$1`, ticket).Scan(&state, &reason); err != nil {
		t.Fatal(err)
	}
	return state, reason
}

func (e *env) events(t *testing.T) map[string]string {
	t.Helper()
	rows, err := e.pool.Query(context.Background(), `SELECT event_id, state || ':' || reason FROM reward_events`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, st string
		if err := rows.Scan(&id, &st); err != nil {
			t.Fatal(err)
		}
		out[id] = st
	}
	return out
}

func (e *env) sync(t *testing.T) {
	t.Helper()
	if err := e.s.Sync(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}
}

func view(taskType string, expires time.Time) TicketView {
	return TicketView{TaskID: 7, TaskTitle: "任务", TaskType: taskType, Period: "monthly", PeriodKey: "2026-11", Status: "in_progress", ExpiresAt: expires}
}

func TestConfigSecretIsSealedAndWriteOnlyIntegration(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	var raw string
	if err := e.pool.QueryRow(ctx, `SELECT value::text FROM settings WHERE key='reward_partner'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, testSecret) || !strings.Contains(raw, "app_secret_enc") {
		t.Fatalf("secret stored in clear: %s", raw)
	}
	// 空 secret 表示不修改。
	if err := e.s.SaveConfig(ctx, ConfigInput{Enabled: true, BaseURL: "https://loxo.cc", AppKey: "rk_2", SDKEnabled: false}); err != nil {
		t.Fatal(err)
	}
	cfg, err := e.s.LoadConfig(ctx)
	if err != nil || cfg.Secret != testSecret || cfg.AppKey != "rk_2" || cfg.BaseURL != "https://loxo.cc" || cfg.SDKEnabled {
		t.Fatalf("config = %+v, %v", cfg, err)
	}
	v, err := e.s.View(ctx)
	if err != nil || !v.SecretSet || v.Summary.Tickets["pending"] != 0 || len(v.Summary.Events) != len(eventStates) {
		t.Fatalf("view = %+v, %v", v, err)
	}
	b, _ := json.Marshal(v)
	if strings.Contains(string(b), testSecret) {
		t.Fatalf("view leaks secret: %s", b)
	}
	// 新库未配置凭证时不能启用。
	_, fresh := testutil.NewMarketDatabase(t)
	err = (&Service{DB: fresh, Keyring: e.s.Keyring}).SaveConfig(ctx, ConfigInput{Enabled: true, AppKey: "rk_1"})
	if err == nil || !strings.Contains(err.Error(), "INVALID_REWARD_CONFIG") {
		t.Fatalf("enable without secret = %v", err)
	}
}

func TestRegisterTicketOnlyRewardsNewUsersIntegration(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	fresh, old := e.user(t, "fresh@example.test"), e.user(t, "old@example.test")
	e.loxo.tickets[ticketA] = view("register", e.now.Add(24*time.Hour))
	e.loxo.tickets[ticketB] = view("register", e.now.Add(24*time.Hour))
	e.loxo.eventReply = func(map[string]any) (int, string, string) { return 200, "", "completed" }

	if ok, err := e.s.Capture(ctx, fresh, ticketA, true); err != nil || !ok {
		t.Fatalf("capture new user = %v %v", ok, err)
	}
	// 注册后登录再绑一次：仍算新用户，不重复建行。
	if ok, err := e.s.Capture(ctx, fresh, ticketA, false); err != nil || !ok {
		t.Fatalf("rebind = %v %v", ok, err)
	}
	// 票据已属于别人。
	if ok, err := e.s.Capture(ctx, old, ticketA, false); err != nil || ok {
		t.Fatalf("steal ticket = %v %v", ok, err)
	}
	if ok, err := e.s.Capture(ctx, old, ticketB, false); err != nil || !ok {
		t.Fatalf("capture old user = %v %v", ok, err)
	}
	e.sync(t)

	if st, _ := e.ticketState(t, ticketA); st != "completed" {
		t.Fatalf("new user ticket = %s", st)
	}
	if st, reason := e.ticketState(t, ticketB); st != "skipped" || reason != reasonNotNewUser {
		t.Fatalf("old user ticket = %s %s", st, reason)
	}
	sent := e.loxo.sent()
	if len(sent) != 1 || sent[0]["event_type"] != "register" || sent[0]["ticket"] != ticketA || sent[0]["external_user_id"] != fmt.Sprint(fresh) {
		t.Fatalf("sent = %v", sent)
	}
	if _, has := sent[0]["amount"]; has {
		t.Fatalf("register event must not carry amount: %v", sent[0])
	}
	// 再同步不会重发。
	e.sync(t)
	if len(e.loxo.sent()) != 1 {
		t.Fatalf("register resent: %v", e.loxo.sent())
	}
}

func TestRechargeWindowAndAccumulationIntegration(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	uid := e.user(t, "buyer@example.test")
	expires := e.now.Add(48 * time.Hour)
	e.loxo.tickets[ticketA] = view("recharge", expires)
	before := e.recharge(t, uid, "50", "credited", e.now.Add(-time.Hour)) // 绑定前支付，不算

	if ok, err := e.s.Capture(ctx, uid, ticketA, false); err != nil || !ok {
		t.Fatal(ok, err)
	}
	e.sync(t) // 票据转 active，还没有可报的充值
	if st, _ := e.ticketState(t, ticketA); st != "active" {
		t.Fatalf("ticket = %s", st)
	}

	e.now = e.now.Add(time.Minute)
	// 绑定后马上下单：渠道回传的支付时间截到整秒，可能早于绑定时间，仍应计入。
	var boundAt time.Time
	if err := e.pool.QueryRow(ctx, `SELECT created_at FROM reward_tickets WHERE ticket=$1`, ticketA).Scan(&boundAt); err != nil {
		t.Fatal(err)
	}
	sameSecond := e.recharge(t, uid, "0.01", "credited", boundAt.Add(time.Second))
	e.exec(t, `UPDATE recharge_orders SET created_at=$2, paid_at=date_trunc('second',$2::timestamptz) WHERE id=$1`, sameSecond, boundAt.Add(time.Millisecond))
	first := e.recharge(t, uid, "30.5", "credited", e.now)
	crediting := e.recharge(t, uid, "70", "crediting", e.now) // 未到账，等下一轮
	late := e.recharge(t, uid, "99", "credited", expires.Add(time.Minute))
	e.exec(t, `UPDATE recharge_orders SET refunded_amount = 0.5 WHERE id=$1`, first)
	total := 0.0
	e.loxo.eventReply = func(in map[string]any) (int, string, string) {
		amt, err := strconv.ParseFloat(in["amount"].(string), 64)
		if err != nil {
			t.Errorf("amount %v: %v", in["amount"], err)
		}
		total += amt
		if total >= 100 {
			return 200, "", "completed"
		}
		return 200, "", "accepted"
	}
	e.sync(t)
	sent := e.loxo.sent()
	if len(sent) != 2 || sent[0]["amount"] != "0.01" || sent[0]["order_id"] != fmt.Sprint(sameSecond) ||
		sent[1]["amount"] != "30.00" || sent[1]["order_id"] != fmt.Sprint(first) || sent[1]["event_id"] != fmt.Sprintf("tm-rc-%d-1", first) {
		t.Fatalf("first sync sent = %v", sent)
	}
	e.exec(t, `UPDATE recharge_orders SET status='credited' WHERE id=$1`, crediting)
	e.sync(t)
	sent = e.loxo.sent()
	if len(sent) != 3 || sent[2]["amount"] != "70.00" {
		t.Fatalf("second sync sent = %v", sent)
	}
	if st, _ := e.ticketState(t, ticketA); st != "completed" {
		t.Fatalf("ticket after reaching 100 = %s", st)
	}
	evs := e.events(t)
	if evs[fmt.Sprintf("tm-rc-%d-1", first)] != "accepted:" || evs[fmt.Sprintf("tm-rc-%d-1", crediting)] != "completed:" {
		t.Fatalf("events = %v", evs)
	}
	for _, id := range []int64{before, late} {
		if _, has := evs[fmt.Sprintf("tm-rc-%d-1", id)]; has {
			t.Fatalf("order %d outside the ticket window was reported: %v", id, evs)
		}
	}
}

func TestEventFailuresRetryAndTerminalIntegration(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	uid := e.user(t, "retry@example.test")
	e.loxo.tickets[ticketA] = view("register", e.now.Add(24*time.Hour))
	if _, err := e.s.Capture(ctx, uid, ticketA, true); err != nil {
		t.Fatal(err)
	}
	// 凭证问题：保留并退避重试。
	e.loxo.eventReply = func(map[string]any) (int, string, string) { return 401, "REWARD_SIGNATURE_INVALID", "" }
	e.sync(t)
	evs := e.events(t)
	if evs["tm-reg-1"] != "pending:REWARD_SIGNATURE_INVALID" {
		t.Fatalf("events = %v", evs)
	}
	// 退避时间未到不重发。
	e.sync(t)
	if n := len(e.loxo.sent()); n != 1 {
		t.Fatalf("resent before backoff: %d", n)
	}
	// 时间到了且 loxo 判定票据被别的账号用过：事件 rejected、票据 invalid。
	e.now = e.now.Add(time.Minute)
	e.loxo.eventReply = func(map[string]any) (int, string, string) { return 409, "REWARD_EXTERNAL_USER_USED", "" }
	e.sync(t)
	if evs := e.events(t); evs["tm-reg-1"] != "rejected:REWARD_EXTERNAL_USER_USED" {
		t.Fatalf("events = %v", evs)
	}
	if st, reason := e.ticketState(t, ticketA); st != "invalid" || reason != "REWARD_EXTERNAL_USER_USED" {
		t.Fatalf("ticket = %s %s", st, reason)
	}
	// 管理员修正后重试：同一个 event_id。
	var eventPK int64
	if err := e.pool.QueryRow(ctx, `SELECT id FROM reward_events WHERE event_id='tm-reg-1'`).Scan(&eventPK); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.RetryEvent(ctx, eventPK); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.RetryEvent(ctx, eventPK); err == nil || !strings.Contains(err.Error(), "REWARD_EVENT_NOT_RETRYABLE") {
		t.Fatalf("retry pending event = %v", err)
	}
	e.loxo.eventReply = func(map[string]any) (int, string, string) { return 409, "REWARD_TASK_QUOTA_EXHAUSTED", "" }
	e.sync(t)
	var next time.Time
	var state string
	if err := e.pool.QueryRow(ctx, `SELECT state, next_attempt_at FROM reward_events WHERE id=$1`, eventPK).Scan(&state, &next); err != nil {
		t.Fatal(err)
	}
	if state != "pending" || next.Sub(e.now) < 5*time.Hour {
		t.Fatalf("quota exhausted must retry later: %s %v", state, next.Sub(e.now))
	}
	sent := e.loxo.sent()
	if len(sent) != 3 || sent[0]["event_id"] != sent[2]["event_id"] {
		t.Fatalf("retries must reuse event_id: %v", sent)
	}
	if _, err := e.s.RetryEvent(ctx, 9999); err == nil || !strings.Contains(err.Error(), "REWARD_EVENT_NOT_FOUND") {
		t.Fatalf("retry missing = %v", err)
	}
}

func TestTicketQueryOutcomesIntegration(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	uid, other := e.user(t, "q@example.test"), e.user(t, "other@example.test")
	e.loxo.tickets[ticketA] = view("browse", e.now.Add(time.Hour))
	bound := view("recharge", e.now.Add(time.Hour))
	bound.ExternalUserID = fmt.Sprint(other)
	e.loxo.tickets[ticketB] = bound
	// ticketC 不存在于 loxo
	for _, tk := range []string{ticketA, ticketB, ticketC} {
		if _, err := e.s.Capture(ctx, uid, tk, false); err != nil {
			t.Fatal(err)
		}
	}
	e.sync(t)
	for tk, want := range map[string]string{
		ticketA: "skipped/" + reasonBrowseTask,
		ticketB: "invalid/REWARD_EXTERNAL_USER_MISMATCH",
		ticketC: "invalid/REWARD_TICKET_NOT_FOUND",
	} {
		if st, reason := e.ticketState(t, tk); st+"/"+reason != want {
			t.Errorf("%s = %s/%s, want %s", tk, st, reason, want)
		}
	}
	if _, err := e.s.Capture(ctx, uid, "not-a-ticket", false); err == nil {
		t.Fatal("invalid ticket accepted")
	}
	page, err := e.s.ListTickets(ctx, "invalid", 1, 20)
	if err != nil || page.Total != 2 || len(page.Items) != 2 || page.Items[0].UserEmail != "q@example.test" {
		t.Fatalf("list = %+v, %v", page, err)
	}
	if _, err := e.s.ListTickets(ctx, "bogus", 1, 20); err == nil {
		t.Fatal("bogus state accepted")
	}
}

func TestLandingAndDisabledIntegration(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.loxo.tickets[ticketA] = view("browse", e.now.Add(time.Hour))
	e.loxo.tickets[ticketB] = view("recharge", e.now.Add(time.Hour))
	got := e.s.Landing(ctx, ticketA)
	if got.Valid == nil || !*got.Valid || got.TaskType != "browse" || got.SDK == nil || !strings.HasSuffix(got.SDK.ScriptURL, "/reward-task-sdk.js") || got.SDK.AppKey != "rk_test" {
		t.Fatalf("browse landing = %+v %+v", got, got.SDK)
	}
	if got := e.s.Landing(ctx, ticketB); got.Valid == nil || !*got.Valid || got.SDK != nil || got.TaskType != "recharge" {
		t.Fatalf("recharge landing = %+v", got)
	}
	if got := e.s.Landing(ctx, ticketC); got.Valid == nil || *got.Valid {
		t.Fatalf("unknown landing = %+v", got)
	}
	queries := e.loxo.queries
	if got := e.s.Landing(ctx, "<script>"); got.Valid == nil || *got.Valid || e.loxo.queries != queries {
		t.Fatalf("malformed landing = %+v (queries %d)", got, e.loxo.queries)
	}

	// 关闭后：不查票据、不绑定、worker 不发请求。
	if err := e.s.SaveConfig(ctx, ConfigInput{Enabled: false, BaseURL: "https://loxo.cc", AppKey: "rk_test"}); err != nil {
		t.Fatal(err)
	}
	if got := e.s.Landing(ctx, ticketA); got.Valid != nil {
		t.Fatalf("disabled landing = %+v", got)
	}
	uid := e.user(t, "off@example.test")
	if ok, err := e.s.Capture(ctx, uid, ticketA, true); err != nil || ok {
		t.Fatalf("disabled capture = %v %v", ok, err)
	}
	e.sync(t)
	if e.loxo.queries != queries {
		t.Fatalf("disabled sync queried loxo: %d", e.loxo.queries)
	}
	if got := e.s.ScriptOrigins(); got != nil {
		t.Fatalf("disabled origins = %v", got)
	}
}

func TestPingIntegration(t *testing.T) {
	e := newEnv(t)
	res, err := e.s.Ping(context.Background())
	if err != nil || res.PartnerName != "闲猫" || res.AppKey != "rk_test" {
		t.Fatalf("ping = %+v, %v", res, err)
	}
}
