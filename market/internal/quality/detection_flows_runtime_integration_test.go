//go:build integration

package quality

import (
	"context"
	"io"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/yx-token/idlicat/market/internal/inspection"
	"github.com/yx-token/idlicat/market/internal/settings"
)

func TestDetectionFlowsPureThirdPartySkipsEmptyExecutionPlan(t *testing.T) {
	t.Parallel()
	pool, run, _ := inspectionDB(t)
	ctx := context.Background()
	run.flows, run.identitySource = []string{"identity"}, "third_party"
	flows := policyFlowsFixture()
	flows.Intelligence.Models = []IntelligenceModel{{Model: run.model}}
	sites := []ThirdPartyCheck{{Provider: "bazaarlink", OnMismatch: "degraded", OnSubmodelMismatch: "suspected"}}
	if _, err := pool.Exec(ctx, `UPDATE detection_policies SET enabled=true,flows=$1,third_party=$2 WHERE platform='anthropic'`, mustJSON(flows), mustJSON(sites)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE detection_providers SET enabled=true WHERE name='bazaarlink'`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE probe_runs SET flows=ARRAY['identity'],identity_source='third_party',status='running' WHERE id=$1`, run.id); err != nil {
		t.Fatal(err)
	}
	// 已完成的外部任务模拟重入：本次没有题目，也不能重复创建检测站任务。
	if _, err := pool.Exec(ctx, `INSERT INTO third_party_checks(run_id,upstream_id,provider,model,format,config,status,outcome,verdict,deadline,finished_at)
		VALUES($1,$2,'bazaarlink',$3,'anthropic',$4,'completed','pass','normal',now()+interval '1 hour',now())`, run.id, run.upstreamID, run.model, mustJSON(sites[0])); err != nil {
		t.Fatal(err)
	}
	e := Engine{DB: pool, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Settings: func(context.Context) (settings.Settings, error) { return settings.Defaults(), nil }}
	// 不配置 Prober 或 seed 密钥，保证纯第三方运行没有进入内部执行底座。
	if err := e.executeTarget(ctx, run, nil, time.Second); err != nil {
		t.Fatal(err)
	}
	if err := e.advanceThirdParty(ctx); err != nil {
		t.Fatal(err)
	}
	var status, identity, verdict string
	var probes, submitted int
	if err := pool.QueryRow(ctx, `SELECT r.status,r.summary->'identity'->>'status',r.summary->>'verdict',
		(SELECT count(*) FROM probe_results WHERE run_id=r.id),(SELECT count(*) FROM third_party_checks WHERE run_id=r.id)
		FROM probe_runs r WHERE id=$1`, run.id).Scan(&status, &identity, &verdict, &probes, &submitted); err != nil ||
		status != "done" || identity != "pass" || verdict != "normal" || probes != 0 || submitted != 1 {
		t.Fatalf("纯第三方应正常结案且不生成内部题或重复任务：%s %s %s %d %d %v", status, identity, verdict, probes, submitted, err)
	}
}

func TestDetectionFlowsScheduleOnlyAdvancesDueFlow(t *testing.T) {
	t.Parallel()
	pool, run, ref := inspectionDB(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DELETE FROM probe_runs WHERE id=ANY($1)`, []int64{run.id, ref.id}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	future := now.Add(12 * time.Hour)
	flows := policyFlowsFixture()
	flows.Intelligence.Models = []IntelligenceModel{{Model: run.model}}
	flows.Identity.Source, flows.Identity.ScheduledEnabled = "self", true
	flows.Identity.Triggers = []string{"scheduled", "admin", "onboarding"}
	if _, err := pool.Exec(ctx, `UPDATE detection_policies SET enabled=true,scheduled_enabled=true,jitter_minutes=0,flows=$1 WHERE platform='anthropic'`, mustJSON(flows)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE upstreams SET status='ready',health='healthy',sub2api_account_id=1 WHERE id=$1`, run.upstreamID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO upstream_quality_state(upstream_id,next_intelligence_at,next_identity_at) VALUES($1,$2,$3)`, run.upstreamID, now.Add(-time.Minute), future); err != nil {
		t.Fatal(err)
	}
	e := Engine{DB: pool, Now: func() time.Time { return now }}
	if err := e.scheduleDue(ctx); err != nil {
		t.Fatal(err)
	}
	var selected []string
	var nextIntelligence, nextIdentity time.Time
	if err := pool.QueryRow(ctx, `SELECT r.flows,q.next_intelligence_at,q.next_identity_at FROM probe_runs r JOIN upstream_quality_state q ON q.upstream_id=r.upstream_id WHERE r.upstream_id=$1`, run.upstreamID).
		Scan(&selected, &nextIntelligence, &nextIdentity); err != nil || !reflect.DeepEqual(selected, []string{"intelligence"}) ||
		!nextIntelligence.Equal(now.Add(360*time.Minute)) || !nextIdentity.Equal(future) {
		t.Fatalf("仅到期的测智应排队并推进时间：%v %s %s %v", selected, nextIntelligence, nextIdentity, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE probe_runs SET status='done',finished_at=now() WHERE upstream_id=$1`, run.upstreamID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE upstream_quality_state SET next_identity_at=$2 WHERE upstream_id=$1`, run.upstreamID, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := e.scheduleDue(ctx); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT flows FROM probe_runs WHERE upstream_id=$1 ORDER BY id DESC LIMIT 1`, run.upstreamID).Scan(&selected); err != nil || !reflect.DeepEqual(selected, []string{"identity"}) {
		t.Fatalf("下一轮应只排队到期的身份流程：%v %v", selected, err)
	}
}

func TestDetectionFlowsOnboardingRequiresEverySelectedFlow(t *testing.T) {
	t.Parallel()
	for _, missing := range []string{"intelligence", "identity"} {
		t.Run(missing, func(t *testing.T) {
			pool, run, _ := inspectionDB(t)
			ctx := context.Background()
			run.trigger = "onboarding"
			if _, err := pool.Exec(ctx, `UPDATE upstreams SET status='verifying' WHERE id=$1`, run.upstreamID); err != nil {
				t.Fatal(err)
			}
			e := Engine{DB: pool}
			u, err := e.loadUpstream(ctx, run.upstreamID)
			if err != nil {
				t.Fatal(err)
			}
			checks := map[string]inspection.Check{"D07": {Status: "pass"}, "A04": {Status: "pass"}}
			key := "D07"
			if missing == "identity" {
				key = "A04"
			}
			checks[key] = inspection.Check{Status: "inconclusive"}
			failed, err := e.failUnobservedRun(ctx, run, u, Assessment{AvailableTotal: 20, AvailableOK: 20, Checks: checks})
			if err != nil || !failed {
				t.Fatalf("请求均成功不能替缺结论的%s流程放行：%t %v", missing, failed, err)
			}
		})
	}
}

func TestDetectionFlowsReferenceWhitelistCoversSelfIdentity(t *testing.T) {
	t.Parallel()
	pool, run, ref := inspectionDB(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DELETE FROM probe_runs WHERE id=ANY($1)`, []int64{run.id, ref.id}); err != nil {
		t.Fatal(err)
	}
	flows := policyFlowsFixture()
	flows.Intelligence.Models = []IntelligenceModel{{Model: run.model}}
	flows.Identity.Source = "self"
	whitelist := []string{run.model, "claude-haiku-4-5"}
	if _, err := pool.Exec(ctx, `UPDATE detection_policies SET enabled=true,flows=$1 WHERE platform='anthropic'`, mustJSON(flows)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE upstreams SET status='ready',health='healthy',sub2api_account_id=id,model_whitelist=$1 WHERE id=ANY($2)`, whitelist, []int64{run.upstreamID, ref.upstreamID}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE upstreams SET is_reference=true,model_whitelist=$2 WHERE id=$1`, ref.upstreamID, []string{run.model}); err != nil {
		t.Fatal(err)
	}
	e := Engine{DB: pool}
	for _, complete := range []bool{false, true} {
		if complete {
			if _, err := pool.Exec(ctx, `UPDATE upstreams SET model_whitelist=$2 WHERE id=$1`, ref.upstreamID, whitelist); err != nil {
				t.Fatal(err)
			}
		}
		id, err := e.QueueRun(ctx, run.upstreamID, "admin", "identity")
		if err != nil {
			t.Fatal(err)
		}
		var references int
		var models []string
		if err := pool.QueryRow(ctx, `SELECT r.models,(SELECT count(*) FROM probe_runs other WHERE other.batch_id=r.batch_id AND other.trigger='reference') FROM probe_runs r WHERE id=$1`, id).Scan(&models, &references); err != nil {
			t.Fatal(err)
		}
		want := 0
		if complete {
			want = 1
		}
		if len(models) != 0 || references != want {
			t.Fatalf("自有身份基线须覆盖全部白名单，测智模型列保持空：完整白名单=%t 模型=%v 基线数=%d", complete, models, references)
		}
	}
}
