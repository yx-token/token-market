//go:build integration

package quality

import (
	"context"
	"testing"

	"github.com/DueGin/token-market/internal/inspection"
)

func TestInspectionFailedVerificationDistinguishesIncompleteAndConfirmed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		total     int
		confirmed bool
		failed    bool
	}{
		{"没有有效观测", 0, false, true},
		{"少量失败不能证明上架通过", 2, false, true},
		{"未校准门槛仍是不完整验证", 100, false, true},
		{"已确认失败不能绕过门槛处置", 100, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool, run, _ := inspectionDB(t)
			ctx := context.Background()
			run.trigger = "onboarding"
			if _, err := pool.Exec(ctx, `UPDATE probe_runs SET status='running',trigger='onboarding' WHERE id=$1`, run.id); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE upstreams SET status='verifying',health='healthy' WHERE id=$1`, run.upstreamID); err != nil {
				t.Fatal(err)
			}
			e := Engine{DB: pool}
			u, err := e.loadUpstream(ctx, run.upstreamID)
			if err != nil {
				t.Fatal(err)
			}
			a := Assessment{InspectionMode: "formal", AvailableTotal: tc.total, Checks: map[string]inspection.Check{"D07": {Status: "inconclusive"}, "A04": {Status: "inconclusive"}}, Gates: []inspection.Gate{{CheckKey: "D13", Confirmed: tc.confirmed, Health: "unavailable"}}}
			if tc.confirmed {
				a.Verdict = "unavailable"
			}
			failed, err := e.failUnobservedRun(ctx, run, u, a)
			if err != nil || failed != tc.failed {
				t.Fatalf("检测未完成与已确认门槛混淆：%v/%v，错误 %v", failed, tc.failed, err)
			}
			var health, status string
			if err := pool.QueryRow(ctx, `SELECT health,status FROM upstreams WHERE id=$1`, u.id).Scan(&health, &status); err != nil {
				t.Fatal(err)
			}
			if health != "healthy" || tc.failed && status != "verify_failed" || !tc.failed && status != "verifying" {
				t.Fatalf("未完成验证只能推进重试流程，不能改变健康：%s/%s", health, status)
			}
		})
	}
}
