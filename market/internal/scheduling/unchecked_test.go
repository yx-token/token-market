package scheduling

import "testing"

func TestUntestedSupplyOnlyUsesExplicitListingGroup(t *testing.T) {
	u := UpstreamInput{ID: 1, Platform: "anthropic", Status: "listed", Health: "unknown", AccountID: 11, ProbeGroupID: i64(1001),
		Listing: &ListingInput{ID: 1, Mode: "consignment", Status: "active", Multiplier: f64(.3), JoinDefaultPool: true, GroupID: i64(5001)}}
	d := Plan(planCtx(), []UpstreamInput{u})[0]
	if !d.BuyerFacing || !equalIDs(d.GroupIDs, []int64{1001, 5001}) {
		t.Fatalf("authorized untested listing should serve explicit buyers and stay out of platform pools: %+v", d)
	}
	for _, state := range []string{"suspended", "removing", "removed", "selecting_models", "verifying"} {
		u.Status = state
		if got := Plan(planCtx(), []UpstreamInput{u})[0]; got.BuyerFacing {
			t.Fatalf("untested supply cannot bypass %s: %+v", state, got)
		}
	}
}
