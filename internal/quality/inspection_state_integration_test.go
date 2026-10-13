//go:build integration

package quality

import (
	"context"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/DueGin/token-market/internal/inspection"
)

func TestInspectionRecoveryRequiresFreshCoverage(t *testing.T) {
	t.Parallel()
	pool, run, _ := inspectionDB(t)
	ctx := context.Background()
	e := Engine{DB: pool}
	u := &upstreamRow{id: run.upstreamID, platform: "anthropic"}
	if _, err := pool.Exec(ctx, `INSERT INTO upstream_quality_state(upstream_id,last_failed_checks) VALUES($1,'{D09,A04}')`, u.id); err != nil {
		t.Fatal(err)
	}
	fresh := func(status string) inspection.Check {
		return inspection.Check{Status: status, Evidence: inspection.Evidence{Source: "paired_probe"}}
	}
	for _, tc := range []struct {
		name   string
		checks map[string]inspection.Check
		want   bool
	}{
		{"全部缺测", map[string]inspection.Check{}, false},
		{"只覆盖一个旧失败项", map[string]inspection.Check{"A04": fresh("pass")}, false},
		{"有一项明确未检测", map[string]inspection.Check{"A04": fresh("pass"), "D09": fresh("not_run")}, false},
		{"有一项仍失败", map[string]inspection.Check{"A04": fresh("pass"), "D09": fresh("fail")}, false},
		{"有一项结论不确定", map[string]inspection.Check{"A04": fresh("pass"), "D09": fresh("inconclusive")}, false},
		{"历史通过不能替代本次验证", map[string]inspection.Check{"A04": fresh("pass"), "D09": {Status: "pass", Evidence: inspection.Evidence{Source: "history"}}}, false},
		{"所有旧失败项本次均通过", map[string]inspection.Check{"A04": fresh("pass"), "D09": fresh("pass")}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := e.inspectionRecoveryComplete(ctx, u, Assessment{Checks: tc.checks})
			if err != nil || got != tc.want {
				t.Fatalf("恢复覆盖判定错误：得到 %v，期望 %v，错误 %v", got, tc.want, err)
			}
			var failed []string
			if err := pool.QueryRow(ctx, `SELECT last_failed_checks FROM upstream_quality_state WHERE upstream_id=$1`, u.id).Scan(&failed); err != nil || !reflect.DeepEqual(failed, []string{"D09", "A04"}) {
				t.Fatalf("只读恢复检查不能清除旧失败项：%v %v", failed, err)
			}
		})
	}
	if _, err := pool.Exec(ctx, `UPDATE upstream_quality_state SET last_failed_checks='{}' WHERE upstream_id=$1`, u.id); err != nil {
		t.Fatal(err)
	}
	if complete, err := e.inspectionRecoveryComplete(ctx, u, Assessment{}); err != nil || !complete {
		t.Fatalf("没有旧失败项不应要求额外覆盖：%v %v", complete, err)
	}
}

func TestInspectionProductionAvailabilityAttributionAndThreshold(t *testing.T) {
	t.Parallel()
	pool, run, _ := inspectionDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	e := Engine{DB: pool, Now: func() time.Time { return now }}
	u := &upstreamRow{id: run.upstreamID, platform: "anthropic"}
	var listingID int64
	if err := pool.QueryRow(ctx, `INSERT INTO listings(upstream_id,seller_id,mode,status,multiplier) SELECT id,seller_id,'consignment','active',0.45 FROM upstreams WHERE id=$1 RETURNING id`, u.id).Scan(&listingID); err != nil {
		t.Fatal(err)
	}
	st := defaultInspectionSettings()
	st.Mode = inspection.ModeFormal
	st.CheckOverrides["D13"] = inspection.CheckOverride{Calibrated: true, CalibrationNote: "离线 SQL 归因夹具，非真实运营校准。", Thresholds: map[string]float64{"min_samples": 5, "gate_min_samples": 5}}
	probe := inspection.Check{Key: "D13", Status: "pass", Factor: numberPointer(1), Metrics: map[string]inspection.Metric{"success_rate": metric(1, "fraction")}, Samples: inspection.Samples{Observed: 3, Paired: 3}, Evidence: inspection.Evidence{Source: "paired_probe", Code: "保留探针证据"}}
	checks := map[string]inspection.Check{"D13": probe}
	gates := []inspection.Gate{}
	insert := func(reason, class string, status int, billable bool, started time.Time) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO edge_attempts(edge_request_id,buyer_key_id,hop,listing_id,http_status,committed,billable,end_reason,error_class,started_at,ended_at) VALUES('离线可用率夹具',1,0,$1,$2,true,$3,$4,$5,$6,$6)`, listingID, status, billable, reason, class, started); err != nil {
			t.Fatal(err)
		}
	}
	for range 4 {
		insert("completed", "", 200, true, now)
	}
	for _, row := range []struct {
		reason, class string
		status        int
	}{
		{"upstream_error", "platform", 429},
		{"upstream_error", "rate_limited", 429},
		{"upstream_error", "configuration", 400},
		{"upstream_error", "unknown", 400},
		// 旧日志的 completed 只表示透传结束；未知 400 不能被当作成功。
		{"completed", "", 400},
		{"client_cancel", "", 200},
		{"skipped", "http_5xx", 503},
	} {
		insert(row.reason, row.class, row.status, true, now)
	}
	insert("completed", "", 200, false, now)
	insert("upstream_error", "http_5xx", 503, true, now.Add(-8*24*time.Hour))
	if err := e.inspectProduction(ctx, u, run.model, st, checks, &gates); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(checks["D13"], probe) || len(gates) != 0 {
		t.Fatalf("排除样本不能凑足阈值或替换探针：%+v %v", checks["D13"], gates)
	}

	// 第五个有效观测为明确的上游 5xx；可用率应为 4/5，而不是含未知 400 的比例。
	insert("upstream_error", "http_5xx", 503, true, now)
	if err := e.inspectProduction(ctx, u, run.model, st, checks, &gates); err != nil {
		t.Fatal(err)
	}
	got := checks["D13"]
	rate := got.Metrics["success_rate"].Value
	if got.Evidence.Source != "production" || got.Samples.Paired != 5 || got.Samples.Excluded != 4 || got.Samples.Observed != 9 || rate == nil || math.Abs(*rate-.8) > 1e-9 {
		t.Fatalf("生产可用率归因或阈值错误：%+v", got)
	}
	if len(gates) != 1 || gates[0].CheckKey != "D13" || !gates[0].Confirmed || gates[0].Health != "unavailable" {
		t.Fatalf("达到门槛样本后应保留已校准可用率门槛：%v", gates)
	}
}

func TestInspectionCacheRequiresBothCompleteStepsAndUsage(t *testing.T) {
	t.Parallel()
	pool, run, ref := inspectionDB(t)
	ctx := context.Background()
	e := Engine{DB: pool}
	item, err := CreateItem(ctx, pool, Item{Suite: "inspection_function", CheckKey: "D12", Category: "缓存证据夹具", Prompt: "只输出 READY。", Grader: Grader{Type: "exact", Answer: "READY"}, MaxTokens: 32})
	if err != nil {
		t.Fatal(err)
	}
	it := plannedInstance{Item: *item, ItemID: item.ID, CheckKey: "D12", Hash: "离线缓存证据实例", GeneratorVersion: "static-1", Steps: []plannedStep{{Step: 0}, {Step: 1}}}
	st := defaultInspectionSettings()
	st.Mode = inspection.ModeFormal
	st.CheckOverrides["D12"] = inspection.CheckOverride{Calibrated: true, CalibrationNote: "离线缓存证据夹具，非真实运营校准。"}
	value := func(n int64) *int64 { return &n }
	type cacheStep struct {
		cache    *int64
		omit     bool
		complete bool
		status   int
	}
	step := func(n int64) cacheStep { return cacheStep{cache: value(n), complete: true, status: 200} }
	for _, tc := range []struct {
		name      string
		target    [2]cacheStep
		reference [2]cacheStep
		noRef     bool
		want      string
		factor    float64
	}{
		{"双侧两步且第二步均命中", [2]cacheStep{step(0), step(100)}, [2]cacheStep{step(0), step(100)}, false, "pass", 1},
		{"基线命中而被测第二步未命中", [2]cacheStep{step(100), step(0)}, [2]cacheStep{step(0), step(100)}, false, "fail", .85},
		{"基线仅第一步命中不适用", [2]cacheStep{step(0), step(100)}, [2]cacheStep{step(100), step(0)}, false, "not_applicable", 0},
		{"被测缺第二步用量", [2]cacheStep{step(0), {complete: true, status: 200}}, [2]cacheStep{step(0), step(100)}, false, "inconclusive", 0},
		{"基线缺第一步用量", [2]cacheStep{step(0), step(100)}, [2]cacheStep{{complete: true, status: 200}, step(100)}, false, "inconclusive", 0},
		{"被测缺第二步记录", [2]cacheStep{step(0), {omit: true}}, [2]cacheStep{step(0), step(100)}, false, "inconclusive", 0},
		{"基线缺第一步记录", [2]cacheStep{step(0), step(100)}, [2]cacheStep{{omit: true}, step(100)}, false, "inconclusive", 0},
		{"被测第二步尚未完成", [2]cacheStep{step(0), {cache: value(100), status: 200}}, [2]cacheStep{step(0), step(100)}, false, "inconclusive", 0},
		{"基线第二步请求失败", [2]cacheStep{step(0), step(100)}, [2]cacheStep{step(0), {cache: value(100), complete: true, status: 503}}, false, "inconclusive", 0},
		{"没有同期基线也能按被测第二步命中判定", [2]cacheStep{step(0), step(100)}, [2]cacheStep{step(0), step(100)}, true, "pass", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, `DELETE FROM probe_results WHERE item_id=$1`, item.ID); err != nil {
				t.Fatal(err)
			}
			for side, rows := range [][2]cacheStep{tc.target, tc.reference} {
				runID := run.id
				if side == 1 {
					runID = ref.id
				}
				for index, row := range rows {
					if row.omit {
						continue
					}
					result := ProbeResult{HTTPStatus: row.status, CacheReadTokens: row.cache, StopReason: "end_turn", Text: "READY"}
					if row.status >= 500 {
						result.ErrorClass = "http_5xx"
					}
					if err := e.saveInstanceResult(ctx, runID, it, index, result, row.status == 200, resumedState{Complete: row.complete, Text: "READY"}); err != nil {
						t.Fatal(err)
					}
				}
			}
			definition, _ := inspection.Effective("D12", st)
			checks := map[string]inspection.Check{"D12": newInspectionCheck(definition, time.Now())}
			reference := &ref
			if tc.noRef {
				reference = nil
			}
			if err := e.inspectCache(ctx, run, reference, executionSnapshot{Instances: []plannedInstance{it}}, st, checks); err != nil {
				t.Fatal(err)
			}
			got := checks["D12"]
			if got.Status != tc.want || tc.factor == 0 && got.Factor != nil || tc.factor != 0 && (got.Factor == nil || math.Abs(*got.Factor-tc.factor) > 1e-9) {
				t.Fatalf("缓存两步判定错误：%+v，期望 %s/%g", got, tc.want, tc.factor)
			}
			applied := inspection.Aggregate(checks, nil, inspection.ModeFormal, st).Checks["D12"].Applied
			if applied != (tc.factor > 0) {
				t.Fatalf("缺失或不适用缓存证据被计入扣价：%+v", got)
			}
		})
	}
}
