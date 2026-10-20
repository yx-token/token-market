//go:build integration

package quality

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/yx-token/idlicat/market/internal/inspection"
)

func TestRecoverySamplesAccumulateOnlyLatestTwoRoundsAfterGate(t *testing.T) {
	t.Parallel()
	pool, run, _ := inspectionDB(t)
	ctx := context.Background()
	st := calibratedInspectionSettings()
	e := Engine{DB: pool}
	u := &upstreamRow{id: run.upstreamID, platform: "anthropic", health: "unavailable"}
	gateTime := time.Now().Add(-time.Hour)
	if _, err := pool.Exec(ctx, `INSERT INTO quality_verdicts(upstream_id,run_id,model,verdict,gates,created_at) VALUES($1,$2,$3,'unavailable','[{"check_key":"D13","confirmed":true}]',$4)`, u.id, run.id, run.model, gateTime); err != nil {
		t.Fatal(err)
	}
	var item int64
	if err := pool.QueryRow(ctx, `INSERT INTO probe_items(suite,category,prompt,grader) VALUES('inspection_authenticity','恢复测试','安全短题','{"type":"exact","answer":"1"}') RETURNING id`).Scan(&item); err != nil {
		t.Fatal(err)
	}
	var runs []int64
	for round := 0; round < 3; round++ {
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO probe_runs(upstream_id,batch_id,trigger,policy_version,model,mode,status,execution_snapshot) VALUES($1,$2,'recovery',1,$3,'full','done',$4) RETURNING id`, u.id, fmt.Sprint("恢复-", round), run.model, mustJSON(map[string]any{"inspection_mode": "formal", "inspection_settings": st})).Scan(&id); err != nil {
			t.Fatal(err)
		}
		runs = append(runs, id)
		// 最旧一轮故障不纳入最近两轮；另有门槛前故障也不得纳入。
		class, status := "", 200
		if round == 0 {
			class, status = "http_5xx", 503
		}
		if _, err := pool.Exec(ctx, `INSERT INTO probe_results(run_id,item_id,suite,check_key,instance_idx,generator_version,http_status,error_class,flags,step_state,created_at) SELECT $1,$2,'inspection_authenticity','D13',n,'availability_recovery/2',$3,NULLIF($4,''),'{window_available}','{"complete":true}',$5 FROM generate_series(1,10) n`, id, item, status, class, gateTime.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO probe_results(run_id,item_id,suite,check_key,instance_idx,generator_version,http_status,error_class,flags,step_state,created_at) VALUES($1,$2,'inspection_authenticity','D13',99,'availability_recovery/2',503,'http_5xx','{window_available}','{"complete":true}',$3)`, id, item, gateTime.Add(-time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	current := run
	current.id = runs[2]
	current.trigger = "recovery"
	checks := map[string]inspection.Check{}
	gates := []inspection.Gate{}
	if err := e.inspectRecoveryAvailability(ctx, current, u, st, checks, &gates); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"D13", "D04"} {
		c := checks[key]
		if c.Status != "pass" || c.Samples.Paired != 20 || c.Evidence.Source != "recovery_probe" {
			t.Fatalf("%s 未正确累计两轮新样本：%+v", key, c)
		}
	}
	if len(gates) != 0 {
		t.Fatalf("旧故障影响恢复：%v", gates)
	}
	// 当前半轮明确失败：累计的旧通过不能盖掉新失败。
	if _, err := pool.Exec(ctx, `UPDATE probe_results SET http_status=503,error_class='http_5xx' WHERE run_id=$1 AND instance_idx=1`, current.id); err != nil {
		t.Fatal(err)
	}
	if err := e.inspectRecoveryAvailability(ctx, current, u, st, checks, &gates); err != nil {
		t.Fatal(err)
	}
	if checks["D13"].Status == "pass" {
		t.Fatal("累计通过掩盖了本次新失败")
	}
	// 本轮全是平台错误时，上一轮通过也不能变成本轮有效恢复。
	if _, err := pool.Exec(ctx, `UPDATE probe_results SET http_status=403,error_class='platform' WHERE run_id=$1`, current.id); err != nil {
		t.Fatal(err)
	}
	if err := e.inspectRecoveryAvailability(ctx, current, u, st, checks, &gates); err != nil {
		t.Fatal(err)
	}
	if checks["D13"].Status != "inconclusive" || checks["D04"].Status != "inconclusive" {
		t.Fatal("本轮没有有效样本，不能继承累计通过来恢复")
	}
	// 新门槛重置窗口，门槛之前的全部样本失效。
	if _, err := pool.Exec(ctx, `UPDATE quality_verdicts SET created_at=now() WHERE upstream_id=$1`, u.id); err != nil {
		t.Fatal(err)
	}
	checks = map[string]inspection.Check{}
	if err := e.inspectRecoveryAvailability(ctx, current, u, st, checks, &gates); err != nil {
		t.Fatal(err)
	}
	if len(checks) != 0 {
		t.Fatal("新门槛未切断恢复历史")
	}
}
