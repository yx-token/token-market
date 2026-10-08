package scheduling

import (
	"testing"

	"github.com/DueGin/token-market/internal/sub2api/opshandlers"
	"github.com/DueGin/token-market/internal/sub2api/replica"
)

func i64(v int64) *int64     { return &v }
func f64(v float64) *float64 { return &v }

func planCtx() PlanContext {
	return PlanContext{
		Pools: []Pool{
			{ID: 1, Name: "tm-pool-claude", Platform: "anthropic", Sub2APIGroupID: i64(100), SellMultiplier: 0.5, MinMargin: 0.1, SafetyBuffer: 0.02, Enabled: true},
			{ID: 2, Name: "tm-pool-all", Platform: "composite", Sub2APIGroupID: i64(200), SellMultiplier: 0.6, MinMargin: 0.1, SafetyBuffer: 0.02, Enabled: true},
			{ID: 3, Name: "tm-pool-gpt", Platform: "openai", Sub2APIGroupID: i64(300), SellMultiplier: 0.5, MinMargin: 0.1, SafetyBuffer: 0.02, Enabled: true},
		},
		Vendors:    map[string]VendorInput{"anthropic": {DefaultConcurrency: 4, ExtraLimits: map[string]any{"max_sessions": 3}}},
		Commission: 0.2,
	}
}

func TestPlanBuybackFirstThenCheapConsignment(t *testing.T) {
	ups := []UpstreamInput{
		{ID: 1, Platform: "anthropic", Status: "listed", Health: "healthy", AccountID: 11, ProbeGroupID: i64(1001), Grade: "A",
			Listing: &ListingInput{ID: 1, Mode: "buyback", Status: "active", Multiplier: f64(0.15)}},
		{ID: 2, Platform: "anthropic", Status: "listed", Health: "healthy", AccountID: 12, ProbeGroupID: i64(1002),
			Listing: &ListingInput{ID: 2, Mode: "consignment", Status: "active", Multiplier: f64(0.30), JoinDefaultPool: true, GroupID: i64(5002)}},
		// 倍率 0.45 超过 tm-pool-claude 上限 0.5×0.88=0.44，只能进 tm-pool-all（上限 0.528）
		{ID: 3, Platform: "anthropic", Status: "listed", Health: "healthy", AccountID: 13, ProbeGroupID: i64(1003),
			Listing: &ListingInput{ID: 3, Mode: "consignment", Status: "active", Multiplier: f64(0.45), JoinDefaultPool: true}},
		// 退出默认分组的寄售只保留在专属分组
		{ID: 4, Platform: "anthropic", Status: "listed", Health: "healthy", AccountID: 14, ProbeGroupID: i64(1004),
			Listing: &ListingInput{ID: 4, Mode: "consignment", Status: "active", Multiplier: f64(0.20), JoinDefaultPool: false, GroupID: i64(5004)}},
	}
	ds := Plan(planCtx(), ups)
	byID := map[int64]Desired{}
	for _, d := range ds {
		byID[d.UpstreamID] = d
	}

	if d := byID[1]; !d.BuyerFacing || d.Priority != 5 || !equalIDs(d.GroupIDs, []int64{100, 200, 1001}) || d.RateMultiplier != 0.15 {
		t.Fatalf("buyback upstream: %+v", d)
	}
	if d := byID[2]; d.Priority != 56 || !equalIDs(d.GroupIDs, []int64{100, 200, 1002, 5002}) {
		t.Fatalf("cheap consignment: %+v", d)
	}
	if d := byID[2]; d.ListingGroupRate == nil || *d.ListingGroupRate != 0.36 {
		t.Fatalf("listing group rate should be 0.30 × 1.2: %+v", d.ListingGroupRate)
	}
	if d := byID[3]; !equalIDs(d.GroupIDs, []int64{200, 1003}) || d.Priority != 59 {
		t.Fatalf("expensive consignment should only join composite pool: %+v", d)
	}
	if d := byID[4]; !equalIDs(d.GroupIDs, []int64{1004, 5004}) {
		t.Fatalf("opted-out consignment should keep only probe + listing groups: %+v", d)
	}
	for _, d := range ds {
		if d.Concurrency != 4 || d.ExtraLimits["max_sessions"] != 3 {
			t.Fatalf("vendor defaults not applied: %+v", d)
		}
		if d.WindowThreshold == nil || *d.WindowThreshold != 100 {
			t.Fatalf("anthropic should get threshold 100 when no reserve: %+v", d.WindowThreshold)
		}
	}
	if byID[1].Priority >= byID[2].Priority {
		t.Fatal("buyback band must be strictly ahead of consignment band")
	}
}

func TestPlanNotBuyerFacing(t *testing.T) {
	base := func(mut func(*UpstreamInput)) UpstreamInput {
		u := UpstreamInput{ID: 1, Platform: "anthropic", Status: "listed", Health: "healthy", AccountID: 11, ProbeGroupID: i64(1001),
			SelfReserve: 30, Listing: &ListingInput{ID: 1, Mode: "consignment", Status: "active", Multiplier: f64(0.3), JoinDefaultPool: true, GroupID: i64(5001)}}
		mut(&u)
		return u
	}
	cases := map[string]UpstreamInput{
		"degraded":    base(func(u *UpstreamInput) { u.Health = "degraded" }),
		"unavailable": base(func(u *UpstreamInput) { u.Health = "unavailable" }),
		"untested":    base(func(u *UpstreamInput) { u.Health = "unknown" }),
		"paused":      base(func(u *UpstreamInput) { u.Listing.Status = "paused" }),
		"suspended":   base(func(u *UpstreamInput) { u.Status = "suspended" }),
		"reference":   base(func(u *UpstreamInput) { u.IsReference = true }),
		"no listing":  base(func(u *UpstreamInput) { u.Listing = nil; u.Status = "ready" }),
	}
	for name, u := range cases {
		d := Plan(planCtx(), []UpstreamInput{u})[0]
		if d.BuyerFacing || d.Priority != priorityIdle || !equalIDs(d.GroupIDs, []int64{1001}) {
			t.Errorf("%s: should only stay in probe group: %+v", name, d)
		}
		if d.WindowThreshold == nil || *d.WindowThreshold != 70 {
			t.Errorf("%s: threshold should be 100 − 30", name)
		}
	}

	if d := Plan(planCtx(), []UpstreamInput{base(func(u *UpstreamInput) { u.Health = "suspected" })})[0]; !d.BuyerFacing || d.Concurrency != 2 {
		t.Errorf("suspected upstream stays buyer facing with halved concurrency: %+v", d)
	}
}

func TestDiffProducesMinimalOpsAndRespectsPending(t *testing.T) {
	thr := 70
	desired := []Desired{
		{UpstreamID: 1, AccountID: 11, BuyerFacing: true, GroupIDs: []int64{100, 1001}, Priority: 5, Concurrency: 4, RateMultiplier: 0.15,
			ExtraLimits: map[string]any{"max_sessions": 3}, WindowThreshold: &thr},
		{UpstreamID: 2, AccountID: 12, BuyerFacing: false, GroupIDs: []int64{1002}, Priority: 99, Concurrency: 4, RateMultiplier: 1},
		{UpstreamID: 3, AccountID: 13, BuyerFacing: true, GroupIDs: []int64{1003}, Priority: 50, Concurrency: 4, RateMultiplier: 0.3},
		{UpstreamID: 4, AccountID: 14, BuyerFacing: true, GroupIDs: []int64{1004}, Priority: 50, Concurrency: 4, RateMultiplier: 0.3,
			ListingGroupID: i64(5004), ListingGroupRate: f64(0.36)},
		{UpstreamID: 5, AccountID: 99, GroupIDs: []int64{1005}},
	}
	actual := Actual{
		Accounts: map[int64]replica.Account{
			11: {ID: 11, Priority: 99, Concurrency: 4, RateMultiplier: 1, Extra: map[string]any{"max_sessions": float64(3)}},
			12: {ID: 12, Priority: 50, Concurrency: 4, RateMultiplier: 1},
			13: {ID: 13, Priority: 1, Concurrency: 1, RateMultiplier: 1},
			14: {ID: 14, Priority: 50, Concurrency: 4, RateMultiplier: 0.3},
		},
		Groups:   map[int64][]int64{11: {1001}, 12: {100, 1002}, 13: {1003}, 14: {1004}},
		GroupRow: map[int64]replica.Group{5004: {ID: 5004, RateMultiplier: 0.3}},
	}
	ops, missing := Diff(DiffInput{
		Desired: desired, Actual: actual,
		Pending:          map[string]bool{"account:13": true},
		AppliedThreshold: map[int64]*int{},
	})
	if len(missing) != 1 || missing[0] != 5 {
		t.Fatalf("missing = %v", missing)
	}
	if len(ops) == 0 || ops[0].Target != "account:12" || !ops[0].Urgent {
		t.Fatalf("removing buyer-facing groups must come first and be urgent: %+v", ops)
	}
	var acc11Update, acc11Bulk, groupUpdate bool
	for _, op := range ops {
		switch {
		case op.Target == "account:13":
			t.Fatal("account with pending ops must be skipped")
		case op.Target == "account:11" && op.Type == opshandlers.TypeAccountUpdate:
			p := op.Payload.(opshandlers.AccountUpdatePayload)
			if p.Input.Concurrency != nil || p.Input.Priority == nil || *p.Input.Priority != 5 || p.Input.GroupIDs == nil {
				t.Fatalf("account 11 update should only carry changed fields: %+v", p.Input)
			}
			acc11Update = true
		case op.Target == "account:11" && op.Type == opshandlers.TypeAccountBulkUpdate:
			if op.AppliedThreshold == nil || *op.AppliedThreshold != 70 {
				t.Fatalf("threshold should be applied: %+v", op)
			}
			acc11Bulk = true
		case op.Target == "group:5004":
			groupUpdate = true
		}
	}
	if !acc11Update || !acc11Bulk || !groupUpdate {
		t.Fatalf("missing expected ops: update=%v bulk=%v group=%v", acc11Update, acc11Bulk, groupUpdate)
	}

	// 已下发且实际状态一致时不再产生操作
	applied := map[int64]*int{1: &thr}
	actual.Accounts[11] = replica.Account{ID: 11, Priority: 5, Concurrency: 4, RateMultiplier: 0.15, Extra: map[string]any{"max_sessions": float64(3)}}
	actual.Groups[11] = []int64{100, 1001}
	ops, _ = Diff(DiffInput{Desired: desired[:1], Actual: actual, AppliedThreshold: applied})
	if len(ops) != 0 {
		t.Fatalf("converged account should produce no ops: %+v", ops)
	}
}

func TestDiffBudget(t *testing.T) {
	var desired []Desired
	actual := Actual{Accounts: map[int64]replica.Account{}, Groups: map[int64][]int64{}}
	for i := int64(1); i <= 5; i++ {
		desired = append(desired, Desired{UpstreamID: i, AccountID: i, Priority: 10, Concurrency: 1, RateMultiplier: 1})
		actual.Accounts[i] = replica.Account{ID: i, Priority: 50, Concurrency: 1, RateMultiplier: 1}
	}
	ops, _ := Diff(DiffInput{Desired: desired, Actual: actual, Budget: 2})
	if len(ops) != 2 {
		t.Fatalf("budget not enforced: %d ops", len(ops))
	}
}
