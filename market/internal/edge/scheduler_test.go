package edge

import (
	"testing"

	"github.com/yx-token/token-market/market/internal/edgecfg"
)

func TestBuildSchedulePlanOrderedFailover(t *testing.T) {
	hops := []edgecfg.Hop{
		{ListingID: 1, Label: "listing:1"},
		{ListingID: 2, Label: "listing:2"},
		{PoolID: 9, Label: "pool:9"},
	}
	skipped := map[int64]bool{2: true}
	blocked := map[string]bool{"pool:9": true}
	plan := BuildSchedulePlan(PolicyOrderedFailover, hops,
		func(id int64) bool { return skipped[id] },
		func(label string) bool { return !blocked[label] },
	)
	if plan.Policy != PolicyOrderedFailover {
		t.Fatalf("policy=%s", plan.Policy)
	}
	if len(plan.Steps) != 3 {
		t.Fatalf("steps=%d", len(plan.Steps))
	}
	if plan.Steps[0].SkipReason != "" || plan.Steps[0].Index != 0 {
		t.Fatalf("step0=%+v", plan.Steps[0])
	}
	if plan.Steps[1].SkipReason != "skip_list" {
		t.Fatalf("step1=%+v", plan.Steps[1])
	}
	if plan.Steps[2].SkipReason != "breaker" {
		t.Fatalf("step2=%+v", plan.Steps[2])
	}
}

func TestBuildSchedulePlanEmptyPolicyDefaults(t *testing.T) {
	plan := BuildSchedulePlan("", nil, nil, nil)
	if plan.Policy != PolicyOrderedFailover {
		t.Fatalf("policy=%s", plan.Policy)
	}
}

func TestSelectSchedulePolicyPoolBuybackFirst(t *testing.T) {
	if p := SelectSchedulePolicy([]edgecfg.Hop{{ListingID: 1}}); p != PolicyOrderedFailover {
		t.Fatalf("listings only: %s", p)
	}
	if p := SelectSchedulePolicy([]edgecfg.Hop{{ListingID: 1}, {PoolID: 3, Label: "pool:x"}}); p != PolicyPoolBuybackFirst {
		t.Fatalf("with pool: %s", p)
	}
}

func TestBuildSchedulePlanKeepsOrderUnderBuybackFirst(t *testing.T) {
	hops := []edgecfg.Hop{
		{ListingID: 1, Label: "listing:1"},
		{PoolID: 3, Label: "pool:3"},
	}
	plan := BuildSchedulePlan(PolicyPoolBuybackFirst, hops, nil, func(string) bool { return true })
	if plan.Policy != PolicyPoolBuybackFirst {
		t.Fatalf("policy=%s", plan.Policy)
	}
	if len(plan.Steps) != 2 || plan.Steps[0].Hop.ListingID != 1 || plan.Steps[1].Hop.PoolID != 3 {
		t.Fatalf("steps=%+v", plan.Steps)
	}
}
