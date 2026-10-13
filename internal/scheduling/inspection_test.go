package scheduling

import (
	"math"
	"testing"

	"github.com/DueGin/token-market/internal/inspection"
)

func inspectionScheduling(mode string, enabled bool, minimum string) inspection.Settings {
	var st inspection.Settings
	st.Mode = mode
	st.Scheduling.Enabled = enabled
	st.Scheduling.MinimumGrade = minimum
	st.Scheduling.Strategy = "quality_adjusted"
	return st
}

func TestQualityPriorityAppliesWithoutFormalPoolGate(t *testing.T) {
	ups := []UpstreamInput{
		{ID: 1, Platform: "anthropic", Status: "listed", Health: "healthy", AccountID: 11, ProbeGroupID: i64(1001), Grade: "C", QualityFactor: 0.5, ConfirmedGate: true,
			Listing: &ListingInput{ID: 1, Mode: "consignment", Status: "active", Multiplier: f64(0.3), JoinDefaultPool: true, GroupID: i64(5001)}},
		{ID: 2, Platform: "anthropic", Status: "listed", Health: "suspected", AccountID: 12, ProbeGroupID: i64(1002), Grade: "B", QualityFactor: 0.75,
			Listing: &ListingInput{ID: 2, Mode: "buyback", Status: "active", Multiplier: f64(0.15)}},
	}
	baseline := Plan(planCtx(), ups)
	for _, st := range []inspection.Settings{inspectionScheduling(inspection.ModeTrial, false, "S"), inspectionScheduling(inspection.ModeTrial, true, "S"), inspectionScheduling(inspection.ModeFormal, false, "S")} {
		pc := planCtx()
		pc.Inspection = st
		got := Plan(pc, ups)
		if got[0].Priority != 62 || got[0].RateMultiplier != baseline[0].RateMultiplier || !equalIDs(got[0].GroupIDs, baseline[0].GroupIDs) {
			t.Fatalf("扣价应改变排序，保障池准入和实际倍率仍遵循独立开关：%+v", got)
		}
		if got[1].Priority != baseline[1].Priority || got[1].Concurrency != baseline[1].Concurrency || !equalIDs(got[1].GroupIDs, baseline[1].GroupIDs) {
			t.Fatalf("扣价不能改变回收等级排序或疑似并发限制：%+v", got)
		}
	}
}

func TestQualityPriorityNeutralFactorPreservesOrder(t *testing.T) {
	ups := []UpstreamInput{{ID: 1, Platform: "anthropic", Status: "listed", Health: "healthy", AccountID: 11, Grade: "B", QualityFactor: 1,
		Listing: &ListingInput{ID: 1, Mode: "consignment", Status: "active", Multiplier: f64(0.3), JoinDefaultPool: true}}}
	pc := planCtx()
	baseline := Plan(pc, ups)[0]
	pc.Inspection = inspectionScheduling(inspection.ModeTrial, false, "S")
	adjusted := Plan(pc, ups)[0]
	if adjusted.Priority != baseline.Priority || adjusted.RateMultiplier != baseline.RateMultiplier || !equalIDs(adjusted.GroupIDs, baseline.GroupIDs) {
		t.Fatalf("没有生效扣价时排序应保持：%+v，%+v", baseline, adjusted)
	}
}

func TestFormalInspectionPoolEligibilityAndQualityPriority(t *testing.T) {
	pc := planCtx()
	pc.Inspection = inspectionScheduling(inspection.ModeFormal, true, "B")
	ups := []UpstreamInput{}
	for i, row := range []struct {
		grade string
		q     float64
		gate  bool
	}{{"A", 1, false}, {"B", 0.75, false}, {"C", 0.8, false}, {"A", 1, true}, {"B", 0, false}} {
		id := int64(i + 1)
		ups = append(ups, UpstreamInput{ID: id, Platform: "anthropic", Status: "listed", Health: "healthy", AccountID: id + 10, ProbeGroupID: i64(1000 + id), Grade: row.grade, QualityFactor: row.q, ConfirmedGate: row.gate,
			Listing: &ListingInput{ID: id, Mode: "consignment", Status: "active", Multiplier: f64(0.3), JoinDefaultPool: true, GroupID: i64(5000 + id)}})
	}
	got := Plan(pc, ups)
	for i, d := range got {
		id := int64(i + 1)
		wantGroups := []int64{100, 200, 1000 + id, 5000 + id}
		if i == 2 || i == 3 {
			wantGroups = []int64{1000 + id, 5000 + id}
		}
		if !d.BuyerFacing || !equalIDs(d.GroupIDs, wantGroups) {
			t.Fatalf("保障池准入不能限制专属分组：%+v", d)
		}
		if d.RateMultiplier != 0.3 || d.ListingGroupRate == nil || *d.ListingGroupRate != 0.36 {
			t.Fatalf("质量策略不能改动实际倍率或费用：%+v", d)
		}
	}
	if got[0].Priority != 56 || got[1].Priority != 58 || got[4].Priority != 56 {
		t.Fatalf("质量价格档计算错误：%+v", got)
	}
	pc.Inspection.Scheduling.MinimumGrade = "A"
	if d := Plan(pc, ups[1:2])[0]; !equalIDs(d.GroupIDs, []int64{1002, 5002}) {
		t.Fatalf("最低等级设置没有生效：%+v", d)
	}
}

func TestFormalInspectionPoolUsesEffectiveGrade(t *testing.T) {
	pc := planCtx()
	pc.Inspection = inspectionScheduling(inspection.ModeFormal, true, "B")
	for _, row := range []struct {
		name         string
		grade        string
		defaultGrade string
		gate         bool
		admitted     bool
		priority     int
	}{
		{"默认等级达到要求", "", "B", false, true, 10},
		{"默认等级低于要求", "", "C", false, false, 15},
		{"已有等级优先于较低默认值", "A", "C", false, true, 5},
		{"已有等级不被较高默认值覆盖", "C", "A", false, false, 15},
		{"没有任何可用等级", "", "", false, false, 19},
		{"默认等级不能绕过确认门槛", "", "A", true, false, 5},
	} {
		t.Run(row.name, func(t *testing.T) {
			u := UpstreamInput{ID: 1, Platform: "anthropic", Status: "listed", Health: "healthy", AccountID: 11, ProbeGroupID: i64(1001), Grade: row.grade, DefaultGrade: row.defaultGrade, QualityFactor: 1, ConfirmedGate: row.gate,
				Listing: &ListingInput{ID: 1, Mode: "buyback", Status: "active", Multiplier: f64(0.15), GroupID: i64(5001)}}
			d := Plan(pc, []UpstreamInput{u})[0]
			wantGroups := []int64{1001, 5001}
			if row.admitted {
				wantGroups = []int64{100, 200, 1001, 5001}
			}
			if !d.BuyerFacing || !equalIDs(d.GroupIDs, wantGroups) || d.Priority != row.priority {
				t.Fatalf("保障池准入与回收排序应使用同一有效等级：%+v", d)
			}
		})
	}
}

func TestQualityAdjustedStrategyBoundsAndFallback(t *testing.T) {
	s := QualityAdjustedStrategy{}
	if s.Name() != "quality_adjusted" || s.BuybackPriority(UpstreamInput{}, "A") != 5 {
		t.Fatal("回收段应保留原确认等级排序")
	}
	for _, q := range []float64{0, -1, 2, math.NaN(), math.Inf(1)} {
		if got := s.ConsignmentPriority(UpstreamInput{QualityFactor: q}, 0.3); got != 56 {
			t.Fatalf("无有效 Q 时须按 1 排序：Q=%v，优先级=%d", q, got)
		}
	}
	if got := s.ConsignmentPriority(UpstreamInput{QualityFactor: 0.5}, 100); got != 79 {
		t.Fatalf("不能跨出寄售优先级段：%d", got)
	}
}
