//go:build integration

package quality

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/yx-token/token-market/market/internal/inspection"
)

func TestModelHistoryUsesCurrentWhitelistAndExpectations(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		models []string
		rate   *float64
		status string
		factor *float64
	}{
		{"撤下失败模型", []string{"继续挂售"}, nil, "pass", numberPointer(1)},
		{"继续挂售失败模型", []string{"已撤下", "继续挂售"}, nil, "fail", numberPointer(.7)},
		{"增加未测模型", []string{"继续挂售", "新挂售"}, nil, "not_run", nil},
		{"修改模型期望", []string{"继续挂售"}, numberPointer(.8), "not_run", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pool, previous, _ := inspectionDB(t)
			ctx := context.Background()
			now := time.Now().UTC()
			st := calibratedInspectionSettings()
			oldPolicy := &Policy{Flows: PolicyFlows{Intelligence: IntelligenceFlowPolicy{FlowPolicy: FlowPolicy{Enabled: true, IntervalMinutes: 360}, Models: []IntelligenceModel{{Model: "已撤下"}, {Model: "继续挂售"}}}}}
			prior := modelHistoryCheck()
			prior.CheckedAt = now.Add(-time.Hour)
			snapshot := executionSnapshot{Settings: &st, Policy: oldPolicy, FlowModes: map[string]string{"intelligence": "full"}}
			if _, err := pool.Exec(ctx, `UPDATE probe_runs SET status='done',finished_at=$2,flows=ARRAY['intelligence'],models=ARRAY['已撤下','继续挂售'],execution_snapshot=$3,summary=$4 WHERE id=$1`, previous.id, prior.CheckedAt, mustJSON(snapshot), mustJSON(map[string]any{"checks": map[string]inspection.Check{"D07": prior}})); err != nil {
				t.Fatal(err)
			}
			policy := &Policy{Flows: PolicyFlows{Intelligence: IntelligenceFlowPolicy{FlowPolicy: oldPolicy.Flows.Intelligence.FlowPolicy}, Identity: IdentityFlowPolicy{FlowPolicy: FlowPolicy{Enabled: true, IntervalMinutes: 1440}, Source: "self"}}}
			for _, model := range tc.models {
				entry := IntelligenceModel{Model: model}
				if model == "继续挂售" {
					entry.ExpectedPassRate = tc.rate
				}
				policy.Flows.Intelligence.Models = append(policy.Flows.Intelligence.Models, entry)
			}
			current := runRow{upstreamID: previous.upstreamID, model: tc.models[0], models: []string{}, mode: "full", trigger: "admin", flows: []string{"identity"}, identitySource: "self"}
			frozen := executionSnapshot{Policy: policy, Models: current.models, ModelWhitelist: tc.models, FlowModes: map[string]string{"identity": "full"}}
			if err := pool.QueryRow(ctx, `INSERT INTO probe_runs(upstream_id,batch_id,trigger,policy_version,model,models,mode,flows,identity_source,execution_snapshot) VALUES($1,'模型历史继承','admin',1,$2,$3,'full',ARRAY['identity'],'self',$4) RETURNING id`, current.upstreamID, current.model, current.models, mustJSON(frozen)).Scan(&current.id); err != nil {
				t.Fatal(err)
			}
			e := Engine{DB: pool, Now: func() time.Time { return now }}
			checks := map[string]inspection.Check{"D07": newInspectionCheck(mustEffective("D07", st), now)}
			if err := e.inheritInspection(ctx, current, &upstreamRow{id: current.upstreamID, modelWhitelist: tc.models}, policy, st, checks); err != nil {
				t.Fatal(err)
			}
			if checks["D07"].Status != tc.status {
				t.Fatalf("按当前模型继承结论错误：%+v", checks["D07"])
			}
			aggregated := inspection.Aggregate(checks, nil, st.Mode, st)
			if (aggregated.QualityFactor == nil) != (tc.factor == nil) || tc.factor != nil && *aggregated.QualityFactor != *tc.factor {
				t.Fatalf("当前模型质量系数错误：%+v", aggregated)
			}
			summary := summarizeFlow("intelligence", current, Assessment{Checks: checks})
			if summary.Status != tc.status {
				t.Fatalf("跨流程摘要结论错误：%+v", summary)
			}
			if tc.status != "not_run" {
				models := []string{}
				for _, result := range summary.Models {
					models = append(models, result.Model)
				}
				if checks["D07"].Evidence.Source != "history" || summary.InheritedAt == nil || !summary.InheritedAt.Equal(prior.CheckedAt) || !reflect.DeepEqual(models, tc.models) {
					t.Fatalf("未展示当前挂售模型的历史结果：%+v", summary)
				}
			} else if summary.InheritedAt != nil || len(summary.Models) != 0 {
				t.Fatalf("被拒绝的历史不应展示模型：%+v", summary)
			}
		})
	}
}

func TestModelHistoryHonorsFlowDefinitionsAndReset(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		flow          string
		mode          string
		previousMode  string
		settingKey    string
		reset         bool
		finishedAfter bool
		checkedAfter  bool
		want          string
	}{
		{name: "跨流程沿用", flow: "identity", mode: "full", previousMode: "full", want: "pass"},
		{name: "快检沿用完整结果", flow: "intelligence", mode: "quick", previousMode: "full", want: "pass"},
		{name: "完整检测必须重测", flow: "intelligence", mode: "full", previousMode: "full", want: "not_run"},
		{name: "快检不能沿用快检", flow: "intelligence", mode: "quick", previousMode: "quick", want: "not_run"},
		{name: "修改其他检测项仍沿用", flow: "identity", mode: "full", previousMode: "full", settingKey: "D09", want: "pass"},
		{name: "修改本项阈值不能沿用", flow: "identity", mode: "full", previousMode: "full", settingKey: "D07", want: "not_run"},
		{name: "扣价重置前的运行不能沿用", flow: "identity", mode: "full", previousMode: "full", reset: true, want: "not_run"},
		{name: "重置后完成仍须检查证据时间", flow: "identity", mode: "full", previousMode: "full", reset: true, finishedAfter: true, want: "not_run"},
		{name: "扣价重置后的新证据可沿用", flow: "identity", mode: "full", previousMode: "full", reset: true, finishedAfter: true, checkedAfter: true, want: "pass"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pool, previous, _ := inspectionDB(t)
			ctx := context.Background()
			now := time.Now().UTC().Truncate(time.Second)
			reset := now.Add(-30 * time.Minute)
			oldSettings, settings := calibratedInspectionSettings(), calibratedInspectionSettings()
			if tc.settingKey != "" {
				override := settings.CheckOverrides[tc.settingKey]
				override.Thresholds = map[string]float64{"expected_pass_rate": .95}
				settings.CheckOverrides[tc.settingKey] = override
			}
			policy := &Policy{Flows: PolicyFlows{Intelligence: IntelligenceFlowPolicy{FlowPolicy: FlowPolicy{Enabled: true, IntervalMinutes: 360}, Models: []IntelligenceModel{{Model: "继续挂售"}}}, Identity: IdentityFlowPolicy{FlowPolicy: FlowPolicy{Enabled: true, IntervalMinutes: 1440}, Source: "self"}}}
			prior, ok := inheritedModelsCheck(modelHistoryCheck(), []string{"继续挂售"})
			if !ok {
				t.Fatal("历史夹具缺少模型结果")
			}
			prior.Evidence.Source = "paired_probe"
			prior.CheckedAt = now.Add(-time.Hour)
			finished := prior.CheckedAt
			if tc.finishedAfter {
				finished = reset.Add(20 * time.Minute)
			}
			if tc.checkedAfter {
				prior.CheckedAt = reset.Add(10 * time.Minute)
			}
			previousSnapshot := executionSnapshot{Settings: &oldSettings, Policy: policy, FlowModes: map[string]string{"intelligence": tc.previousMode}}
			if _, err := pool.Exec(ctx, `UPDATE probe_runs SET status='done',finished_at=$2,flows=ARRAY['intelligence'],models=ARRAY['继续挂售'],mode=$3,execution_snapshot=$4,summary=$5 WHERE id=$1`, previous.id, finished, tc.previousMode, mustJSON(previousSnapshot), mustJSON(map[string]any{"checks": map[string]inspection.Check{"D07": prior}})); err != nil {
				t.Fatal(err)
			}
			if tc.reset {
				if _, err := pool.Exec(ctx, `INSERT INTO upstream_quality_state(upstream_id,deductions_reset_at) VALUES($1,$2)`, previous.upstreamID, reset); err != nil {
					t.Fatal(err)
				}
			}
			current := runRow{upstreamID: previous.upstreamID, model: "继续挂售", models: []string{}, mode: tc.mode, trigger: "admin", flows: []string{tc.flow}, identitySource: "self"}
			if tc.flow == "intelligence" {
				current.models = []string{"继续挂售"}
			}
			frozen := executionSnapshot{Policy: policy, Models: current.models, FlowModes: map[string]string{tc.flow: tc.mode}}
			if err := pool.QueryRow(ctx, `INSERT INTO probe_runs(upstream_id,batch_id,trigger,policy_version,model,models,mode,flows,identity_source,execution_snapshot) VALUES($1,'模型继承范围','admin',1,$2,$3,$4,$5,'self',$6) RETURNING id`, current.upstreamID, current.model, current.models, current.mode, current.flows, mustJSON(frozen)).Scan(&current.id); err != nil {
				t.Fatal(err)
			}
			e := Engine{DB: pool, Now: func() time.Time { return now }}
			checks := map[string]inspection.Check{"D07": newInspectionCheck(mustEffective("D07", settings), now)}
			if err := e.inheritInspection(ctx, current, &upstreamRow{id: current.upstreamID}, policy, settings, checks); err != nil {
				t.Fatal(err)
			}
			if got := checks["D07"]; got.Status != tc.want || (got.Evidence.Source == "history") != (tc.want == "pass") {
				t.Fatalf("历史定义或重置边界错误：%+v", got)
			}
		})
	}
}
