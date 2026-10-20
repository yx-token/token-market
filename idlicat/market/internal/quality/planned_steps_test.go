package quality

import "testing"

// 计划总题数按「实例 × 步骤」求和，与 probe_results 每步一行一致；没有实例时为空，界面显示「—」。
func TestPlannedSteps(t *testing.T) {
	if got := plannedSteps(executionSnapshot{}); got != nil {
		t.Fatalf("没有实例应为空：%v", *got)
	}
	plan := executionSnapshot{Instances: []plannedInstance{
		{Steps: []plannedStep{{}, {}}},
		{Steps: []plannedStep{{}}},
		{Steps: []plannedStep{{}, {}, {}}},
	}}
	if got := plannedSteps(plan); got == nil || *got != 6 {
		t.Fatalf("多实例多步骤应为 6：%v", got)
	}
}
