//go:build integration

package quality

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DueGin/token-market/internal/settings"
)

func TestUnobservedRunDoesNotVerifyOrResolve(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		trigger, health, errorClass, body string
		status                            int
	}{
		{"onboarding", "unknown", "platform", `{"error":{"code":"gateway_concurrency_limit","message":"并发槽已满"}}`, 429},
		{"onboarding", "unknown", "rate_limited", `{"error":{"type":"rate_limit_error","message":"窗口限流"}}`, 429},
		{"onboarding", "unknown", "configuration", `{"error":{"type":"invalid_request_error","message":"不支持该参数"}}`, 400},
		{"appeal", "degraded", "platform", `{"error":{"code":"gateway_concurrency_limit","message":"并发槽已满"}}`, 429},
		{"recovery", "unavailable", "rate_limited", `{"error":{"type":"rate_limit_error","message":"窗口限流"}}`, 429},
		{"scheduled", "healthy", "platform", `{"error":{"code":"gateway_concurrency_limit","message":"并发槽已满"}}`, 429},
		{"adaptive", "suspected", "configuration", `{"error":{"type":"invalid_request_error","message":"不支持该参数"}}`, 400},
	} {
		t.Run(tc.trigger+"_"+tc.errorClass, func(t *testing.T) {
			pool, run, ref := inspectionDB(t)
			ctx := context.Background()
			now := time.Now().UTC().Truncate(time.Second)
			e := Engine{DB: pool, Keyring: testInspectionKeyring(t), Now: func() time.Time { return now }, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Settings: func(context.Context) (settings.Settings, error) { return settings.Defaults(), nil }}
			upstreamStatus := "ready"
			if tc.trigger == "onboarding" {
				upstreamStatus = "verifying"
			}
			if _, err := pool.Exec(ctx, `UPDATE upstreams SET status=$2,health=$3,health_reason='保留原原因',grade='B',probe_group_id=101 WHERE id=$1`, run.upstreamID, upstreamStatus, tc.health); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE probe_runs SET trigger=$2 WHERE id=$1`, run.id, tc.trigger); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `DELETE FROM probe_runs WHERE id=$1`, ref.id); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `INSERT INTO upstream_quality_state(upstream_id,recovery_attempts,consecutive_normal) VALUES($1,2,1)`, run.upstreamID); err != nil {
				t.Fatal(err)
			}
			key, err := e.Keyring.SealString("仅本地替身使用的探针密钥", "system-key:101")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `INSERT INTO system_keys(purpose,sub2api_group_id,key_enc,sub2api_key_id) VALUES('probe',101,$1,1)`, key); err != nil {
				t.Fatal(err)
			}
			if _, err := CreateItem(ctx, pool, Item{Suite: "iq", Category: "有效观测回归", Prompt: "只输出 1。", Grader: Grader{Type: "exact", Answer: "1"}, MaxTokens: 32}); err != nil {
				t.Fatal(err)
			}
			thirdParty := []ThirdPartyCheck{}
			if tc.trigger == "onboarding" || tc.trigger == "appeal" || tc.trigger == "recovery" {
				// 第三方即使启用，也不能替无观测的内部检测放行。
				thirdParty = append(thirdParty, ThirdPartyCheck{Provider: "bazaarlink", Triggers: []string{tc.trigger}, OnMismatch: "degraded", OnSubmodelMismatch: "suspected"})
			}
			if _, err := pool.Exec(ctx, `UPDATE detection_policies SET enabled=true,suites='{iq}',third_party=$1 WHERE platform='anthropic'`, mustJSON(thirdParty)); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE detection_providers SET enabled=true WHERE name='bazaarlink'`); err != nil {
				t.Fatal(err)
			}
			if tc.trigger == "appeal" {
				var reportID, oldRunID int64
				if err := pool.QueryRow(ctx, `INSERT INTO probe_runs(upstream_id,batch_id,trigger,policy_version,model,status) VALUES($1,'旧申诉报告','admin',1,'claude-sonnet-4-5','done') RETURNING id`, run.upstreamID).Scan(&oldRunID); err != nil {
					t.Fatal(err)
				}
				if err := pool.QueryRow(ctx, `INSERT INTO test_reports(upstream_id,run_id,trigger,verdict,summary,impact) VALUES($1,$2,'admin','degraded','{}','{}') RETURNING id`, run.upstreamID, oldRunID).Scan(&reportID); err != nil {
					t.Fatal(err)
				}
				if _, err := pool.Exec(ctx, `INSERT INTO appeals(report_id,seller_id,reason,status) SELECT $1,seller_id,'申请复测','retesting' FROM upstreams WHERE id=$2`, reportID, run.upstreamID); err != nil {
					t.Fatal(err)
				}
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "3600")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			e.Prober = &Prober{GatewayURL: server.URL, Client: server.Client()}
			deadlineCtx, cancel := context.WithTimeout(ctx, time.Minute)
			defer cancel()
			if err := e.ExecuteBatch(deadlineCtx, run.batch); err != nil {
				t.Fatal(err)
			}
			var status, health, reason, grade, runStatus, verdict, failure string
			var count, reportCount, verdictCount, thirdPartyCount, attempts, consecutive int
			var nextRecovery *time.Time
			if err := pool.QueryRow(ctx, `SELECT u.status,u.health,u.health_reason,u.grade,COALESCE(u.failure_reason,''),r.status,r.summary->>'verdict',
 COALESCE((r.summary->'error_counts'->>$3)::int,0),
 (SELECT count(*) FROM test_reports WHERE run_id=r.id),
 (SELECT count(*) FROM quality_verdicts WHERE run_id=r.id),
 (SELECT count(*) FROM third_party_checks WHERE run_id=r.id),q.recovery_attempts,q.consecutive_normal,q.next_recovery_at
 FROM upstreams u JOIN probe_runs r ON r.upstream_id=u.id JOIN upstream_quality_state q ON q.upstream_id=u.id WHERE u.id=$1 AND r.id=$2`, run.upstreamID, run.id, tc.errorClass).
				Scan(&status, &health, &reason, &grade, &failure, &runStatus, &verdict, &count, &reportCount, &verdictCount, &thirdPartyCount, &attempts, &consecutive, &nextRecovery); err != nil {
				t.Fatal(err)
			}
			if health != tc.health || reason != "保留原原因" || grade != "B" || count != 1 || verdict != "inconclusive" || thirdPartyCount != 0 {
				t.Fatalf("零观测改变健康/等级、丢失故障或派发第三方：%s %s %s %s 错误=%d 第三方=%d", health, reason, grade, verdict, count, thirdPartyCount)
			}
			if tc.trigger == "scheduled" || tc.trigger == "adaptive" {
				if runStatus != "done" || reportCount != 1 || verdictCount != 1 || attempts != 2 || consecutive != 1 {
					t.Fatalf("定时/追加应只留下 inconclusive：%s 报告=%d 判定=%d 退避=%d 正常=%d", runStatus, reportCount, verdictCount, attempts, consecutive)
				}
				return
			}
			var errorText string
			if err := pool.QueryRow(ctx, `SELECT summary->>'error' FROM probe_runs WHERE id=$1`, run.id).Scan(&errorText); err != nil {
				t.Fatal(err)
			}
			if runStatus != "failed" || reportCount != 0 || verdictCount != 0 || !strings.Contains(errorText, "没有有效观测：限流/平台错误，可稍后重试") {
				t.Fatalf("必须按检测未完成推进流程：%s 报告=%d 判定=%d 原因=%s", runStatus, reportCount, verdictCount, errorText)
			}
			switch tc.trigger {
			case "onboarding":
				if status != "verify_failed" || !strings.Contains(failure, "没有有效观测") || (tc.errorClass == "configuration" && !strings.Contains(failure, "配置错误")) {
					t.Fatalf("无观测上架检测不能通过：%s %s", status, failure)
				}
			case "appeal":
				var appealStatus string
				var resolved *time.Time
				if err := pool.QueryRow(ctx, `SELECT status,resolved_at FROM appeals WHERE report_id IN (SELECT id FROM test_reports WHERE upstream_id=$1)`, run.upstreamID).Scan(&appealStatus, &resolved); err != nil || appealStatus != "open" || resolved != nil {
					t.Fatalf("申诉应回到待处理：%s %v %v", appealStatus, resolved, err)
				}
			case "recovery":
				want := now.Add(recoveryBackoff[2])
				if nextRecovery == nil || !nextRecovery.Equal(want) || attempts != 3 || consecutive != 1 {
					t.Fatalf("恢复复测应保留健康并退避：%v %d %d，期望 %s", nextRecovery, attempts, consecutive, want)
				}
			}
		})
	}
}
