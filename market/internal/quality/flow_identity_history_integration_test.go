//go:build integration

package quality

import (
	"context"
	"testing"
	"time"

	"github.com/yx-token/token-market/market/internal/inspection"
)

func TestIdentityHistoryRequiresSameModelsAndProviderRules(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, key, source                            string
		changeModel, changeWhitelist, changeProvider bool
		want                                         string
	}{
		{name: "同型号权限沿用", key: "A04", source: "self", want: "pass"},
		{name: "主型号变化重测", key: "A04", source: "self", changeModel: true, want: "not_run"},
		{name: "白名单变化重测", key: "A04", source: "self", changeWhitelist: true, want: "not_run"},
		{name: "第三方配置相同沿用", key: "third_party", source: "third_party", want: "pass"},
		{name: "第三方型号配置变化重测", key: "third_party", source: "third_party", changeProvider: true, want: "not_run"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pool, priorRun, _ := inspectionDB(t)
			ctx := context.Background()
			now := time.Now().UTC()
			st := defaultInspectionSettings()
			p := inspectionTestPolicy(priorRun)
			p.Flows.Identity.Source = tc.source
			p.ThirdParty = []ThirdPartyCheck{{Provider: "bazaarlink", Model: "第三方型号甲", OnMismatch: "degraded"}}
			oldSnapshot := executionSnapshot{Policy: p, Settings: &st, FlowModes: map[string]string{"identity": "full"}, ModelWhitelist: []string{priorRun.model}}
			priorCheck := newInspectionCheck(mustEffective(tc.key, st), now.Add(-time.Hour))
			priorCheck.Status, priorCheck.Samples.Observed = "pass", 1
			if _, err := pool.Exec(ctx, `UPDATE probe_runs SET flows=ARRAY['identity'],identity_source=$2,status='done',finished_at=$3,execution_snapshot=$4,summary=$5 WHERE id=$1`, priorRun.id, tc.source, priorCheck.CheckedAt, mustJSON(oldSnapshot), mustJSON(map[string]any{"checks": map[string]inspection.Check{tc.key: priorCheck}})); err != nil {
				t.Fatal(err)
			}
			current := priorRun
			current.flows, current.models = []string{"intelligence"}, []string{priorRun.model}
			if tc.changeModel {
				current.model = "claude-haiku-4-5"
			}
			whitelist := []string{priorRun.model}
			if tc.changeWhitelist {
				whitelist = append(whitelist, "claude-haiku-4-5")
			}
			// 旧快照已写入，后续策略修改不能回写旧证据的检测站配置。
			if tc.changeProvider {
				p.ThirdParty[0].Model = "第三方型号乙"
			}
			newSnapshot := executionSnapshot{Policy: p, Settings: &st, FlowModes: map[string]string{"intelligence": "full"}, ModelWhitelist: whitelist}
			if err := pool.QueryRow(ctx, `INSERT INTO probe_runs(upstream_id,batch_id,trigger,policy_version,model,mode,flows,models,execution_snapshot) VALUES($1,'身份历史边界','admin',1,$2,'full',ARRAY['intelligence'],$3,$4) RETURNING id`, current.upstreamID, current.model, current.models, mustJSON(newSnapshot)).Scan(&current.id); err != nil {
				t.Fatal(err)
			}
			checks := map[string]inspection.Check{tc.key: newInspectionCheck(mustEffective(tc.key, st), now)}
			e := Engine{DB: pool, Now: func() time.Time { return now }}
			if err := e.inheritInspection(ctx, current, &upstreamRow{id: current.upstreamID, modelWhitelist: whitelist}, p, st, checks); err != nil {
				t.Fatal(err)
			}
			if checks[tc.key].Status != tc.want {
				t.Fatalf("身份历史型号或检测站配置边界错误：%+v，期望=%s", checks[tc.key], tc.want)
			}
		})
	}
}
