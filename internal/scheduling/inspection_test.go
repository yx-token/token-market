package scheduling

import (
	"math"
	"reflect"
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

func TestInspectionDisabledAndTrialPreservePlan(t *testing.T) {
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
		if got := Plan(pc, ups); !reflect.DeepEqual(got, baseline) {
			t.Fatalf("试运行或开关关闭时规划器输出改变：%+v\n%+v", baseline, got)
		}
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
	unknown := ups[0]
	unknown.Grade, unknown.DefaultGrade = "", "A"
	if d := Plan(pc, []UpstreamInput{unknown})[0]; !equalIDs(d.GroupIDs, []int64{1001, 5001}) {
		t.Fatalf("未检测等级不能用默认等级进入保障池：%+v", d)
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
