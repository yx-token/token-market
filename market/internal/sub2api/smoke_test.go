//go:build smoke

// 针对真实运行的 sub2api 的冒烟测试，验证客户端与 sub2api 的接口约定，以及 Edge 对账依赖的行为。
// 是后续 s2a-compat 行为契约阶段的雏形。
//
//	TM_SMOKE_SUB2API_URL     sub2api 地址，如 http://127.0.0.1:58080
//	TM_SMOKE_USER_EMAIL      用于用户 API 的账号
//	TM_SMOKE_USER_PASSWORD
//	TM_SMOKE_GROUP_ID        内部 key 绑定的分组
//	TM_SMOKE_REPLICA_DSN     sub2api 数据库（只读）
//	TM_SMOKE_ADMIN_API_KEY   可选；设置后测试 Admin API（需要管理员已完成 sub2api 合规确认）
package sub2api_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yx-token/idlicat/market/internal/sub2api/adminclient"
	"github.com/yx-token/idlicat/market/internal/sub2api/userclient"
	"github.com/jackc/pgx/v5"
)

func env(t *testing.T, name string) string {
	t.Helper()
	v := os.Getenv(name)
	if v == "" {
		t.Skipf("%s not set", name)
	}
	return v
}

func randomKey() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "tm_smoke_" + hex.EncodeToString(b)
}

func TestUserAPIAndGatewayRequestID(t *testing.T) {
	base := env(t, "TM_SMOKE_SUB2API_URL")
	email := env(t, "TM_SMOKE_USER_EMAIL")
	password := env(t, "TM_SMOKE_USER_PASSWORD")
	groupID, err := strconv.ParseInt(env(t, "TM_SMOKE_GROUP_ID"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	uc := userclient.New(base, 10*time.Second, nil)

	tok, err := uc.Login(ctx, email, password)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	refreshed, err := uc.Refresh(ctx, tok.RefreshToken)
	if err != nil || refreshed.AccessToken == "" || refreshed.RefreshToken == "" {
		t.Fatalf("refresh: %v %+v", err, refreshed)
	}

	custom := randomKey()
	key, err := uc.CreateKey(ctx, refreshed.AccessToken, userclient.CreateKeyInput{Name: "tm-ik-smoke", GroupID: groupID, CustomKey: custom})
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	t.Cleanup(func() { _ = uc.DeleteKey(context.Background(), refreshed.AccessToken, key.ID) })
	if key.Key != custom || key.GroupID == nil || *key.GroupID != groupID {
		t.Fatalf("created key mismatch: %+v", key)
	}

	// 同一 custom_key 重复创建必须失败（据此实现重试幂等）
	if _, err := uc.CreateKey(ctx, refreshed.AccessToken, userclient.CreateKeyInput{Name: "dup", GroupID: groupID, CustomKey: custom}); err == nil {
		t.Fatal("duplicate custom key should be rejected")
	}

	inactive := "inactive"
	if _, err := uc.UpdateKey(ctx, refreshed.AccessToken, key.ID, userclient.UpdateKeyInput{Status: inactive}); err != nil {
		t.Fatalf("update key: %v", err)
	}
	if _, err := uc.UpdateKey(ctx, refreshed.AccessToken, key.ID, userclient.UpdateKeyInput{Status: "active"}); err != nil {
		t.Fatalf("reactivate key: %v", err)
	}

	// Edge 对账依赖：网关的错误响应也要带 X-Client-Request-ID，且失败请求不写 usage_log
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/messages",
		strings.NewReader(`{"model":"claude-sonnet-4-5","max_tokens":8,"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("x-api-key", custom)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	requestID := resp.Header.Get("X-Client-Request-ID")
	t.Logf("gateway status=%d X-Client-Request-ID=%s", resp.StatusCode, requestID)
	if resp.StatusCode < 400 {
		t.Fatalf("group without accounts should fail, got %d", resp.StatusCode)
	}
	if requestID == "" {
		t.Fatal("error response must carry X-Client-Request-ID")
	}

	dsn := os.Getenv("TM_SMOKE_REPLICA_DSN")
	if dsn == "" {
		return
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	time.Sleep(2 * time.Second) // 用量记录是异步写入的
	var n int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM usage_logs WHERE request_id = $1", "client:"+requestID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("failed request must not produce usage_logs, found %d", n)
	}
}

func TestAdminAPI(t *testing.T) {
	base := env(t, "TM_SMOKE_SUB2API_URL")
	apiKey := env(t, "TM_SMOKE_ADMIN_API_KEY")
	ctx := context.Background()
	ac := adminclient.New(base, apiKey, 10*time.Second)

	version, err := ac.GetVersion(ctx)
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	t.Logf("sub2api version %s", version)

	suffix := randomKey()[9:17]
	grp, err := ac.CreateGroup(ctx, adminclient.CreateGroupInput{Name: "tm-smoke-" + suffix, Platform: "anthropic", RateMultiplier: 1.2, IsExclusive: true})
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	t.Cleanup(func() { _ = ac.DeleteGroup(context.Background(), grp.ID) })

	user, err := ac.CreateUser(ctx, adminclient.CreateUserInput{Email: "b-" + suffix + "@shadow.local", Password: randomKey(), Concurrency: 2, AllowedGroups: []int64{grp.ID}})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	opKey := "smoke-balance-" + suffix
	u1, err := ac.UpdateBalance(ctx, user.ID, adminclient.BalanceAdd, 5, "smoke", opKey)
	if err != nil {
		t.Fatalf("balance add: %v", err)
	}
	u2, err := ac.UpdateBalance(ctx, user.ID, adminclient.BalanceAdd, 5, "smoke", opKey)
	if err != nil {
		t.Fatalf("balance add replay: %v", err)
	}
	if u1.Balance != u2.Balance {
		t.Fatalf("idempotent replay changed balance: %v → %v", u1.Balance, u2.Balance)
	}
	if _, err := ac.UpdateBalance(ctx, user.ID, adminclient.BalanceSubtract, 1000, "smoke", opKey+"-sub"); err == nil {
		t.Fatal("subtracting more than the balance must fail")
	}

	rate := 0.3
	acc, err := ac.CreateAccount(ctx, adminclient.CreateAccountInput{
		Name: "tm-u-smoke-" + suffix, Platform: "anthropic", Type: "apikey",
		Credentials: map[string]any{"api_key": "sk-ant-smoke-not-real"},
		Concurrency: 1, Priority: 50, RateMultiplier: &rate, GroupIDs: []int64{grp.ID},
	}, "smoke-account-"+suffix)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	t.Cleanup(func() { _ = ac.DeleteAccount(context.Background(), acc.ID) })

	prio := 10
	if _, err := ac.BulkUpdateAccounts(ctx, adminclient.BulkUpdateAccountsInput{AccountIDs: []int64{acc.ID}, Priority: &prio, Extra: map[string]any{"max_sessions": 2}}); err != nil {
		t.Fatalf("bulk update: %v", err)
	}
	if err := ac.SetSchedulable(ctx, acc.ID, false); err != nil {
		t.Fatalf("set schedulable: %v", err)
	}
	empty := []int64{}
	if _, err := ac.UpdateAccount(ctx, acc.ID, adminclient.UpdateAccountInput{GroupIDs: &empty}); err != nil {
		t.Fatalf("clear groups: %v", err)
	}
}
