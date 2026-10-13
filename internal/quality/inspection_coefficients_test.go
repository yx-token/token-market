package quality

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/DueGin/token-market/internal/inspection"
)

func configuredFunctionPricing(key, mode string, warning, severe float64) InspectionSettings {
	st := defaultInspectionSettings()
	st.Mode = mode
	pricing := true
	st.CheckOverrides[key] = inspection.CheckOverride{Pricing: &pricing, Coefficients: map[string]float64{"warning": warning, "severe": severe, "failure": .99}}
	return st
}

func assertFunctionPrice(t *testing.T, key, status string, want float64, st InspectionSettings, checks map[string]inspection.Check, gates []inspection.Gate) {
	t.Helper()
	result := inspection.Aggregate(checks, gates, st.Mode, st)
	check := result.Checks[key]
	if check.Status != status || check.Factor == nil || math.Abs(*check.Factor-want) > 1e-12 || !check.Applied || result.QualityFactor == nil || math.Abs(*result.QualityFactor-want) > 1e-12 {
		t.Fatalf("管理员状态系数未作用于实际 Q，%s/%s 期望=%v：%+v，Q=%v", key, status, want, check, result.QualityFactor)
	}
}

func TestFunctionConfiguredSevereChangesActualQualityFactor(t *testing.T) {
	for _, mode := range []string{inspection.ModeTrial, inspection.ModeFormal} {
		for _, paired := range []bool{false, true} {
			for _, key := range []string{"D08", "A11.tools", "A11.json_schema", "A11.multiturn", "A11.image_pdf", "A11.thinking"} {
				for _, severe := range []float64{.91, .81} {
					t.Run(mode+"/"+key+map[bool]string{false: "/自身", true: "/配对"}[paired], func(t *testing.T) {
						st := configuredFunctionPricing(key, mode, .97, severe)
						o := pairedInspectionOutcome(key, false, true)
						o.Target.ReasoningTokens = 0
						o.ProbeOutputTokens = 1000
						if !paired {
							o.Ref = nil
						}
						supported := true
						capability := inspection.ModelCapability{Tools: &supported, JSONSchema: &supported, MultiTurn: &supported, Image: &supported, PDF: &supported, Thinking: &supported, MaxOutput: 2000}
						checks, gates := Inspect([]Outcome{o}, st, time.Time{}, InspectOptions{Capability: &capability})
						assertFunctionPrice(t, key, "fail", severe, st, checks, gates)
					})
				}
			}
		}
	}
}

func TestCacheConfiguredWarningAndSevereChangeActualQualityFactor(t *testing.T) {
	for _, mode := range []string{inspection.ModeTrial, inspection.ModeFormal} {
		for _, tc := range []struct {
			name   string
			ref    *runRow
			steps  map[int64]*[2]int64
			status string
			want   float64
		}{
			{"自身未命中使用warning", nil, map[int64]*[2]int64{1: {}}, "warning", .92},
			{"配对未命中使用severe", &runRow{id: 2}, map[int64]*[2]int64{1: {}, 2: {0, 40}}, "fail", .81},
			{"自身命中解除扣价", nil, map[int64]*[2]int64{1: {0, 40}}, "pass", 1},
			{"配对命中解除扣价", &runRow{id: 2}, map[int64]*[2]int64{1: {0, 40}, 2: {0, 40}}, "pass", 1},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				st := configuredFunctionPricing("D12", mode, .92, .81)
				d, _ := inspection.Effective("D12", st)
				plan := executionSnapshot{Instances: []plannedInstance{{ItemID: 1, CheckKey: "D12", Steps: make([]plannedStep, 2)}}}
				checks := map[string]inspection.Check{"D12": newInspectionCheck(d, time.Time{})}
				loader := cacheStepLoader{steps: tc.steps}
				if err := inspectCacheChecks(context.Background(), runRow{id: 1}, tc.ref, plan, st, checks, loader.load, inspection.ModelCapability{PromptCache: capabilityBoolPointer(true)}); err != nil {
					t.Fatal(err)
				}
				assertFunctionPrice(t, "D12", tc.status, tc.want, st, checks, nil)
			})
		}
	}
}
