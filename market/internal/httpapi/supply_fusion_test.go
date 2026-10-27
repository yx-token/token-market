package httpapi

import (
	"testing"

	"github.com/yx-token/token-market/market/internal/listing"
	"github.com/yx-token/token-market/market/internal/scheduling"
	"github.com/yx-token/token-market/market/internal/sub2api/replica"
)

func i64(v int64) *int64 { return &v }

func TestFuseAccountsJoinsUpstreamToEngine(t *testing.T) {
	ups := []listing.Upstream{{ID: 7, SellerID: 3, Platform: "openai", Status: "listed", Health: "healthy", Sub2APIAccountID: i64(42)}}
	engines := []replica.Account{
		{ID: 42, Name: "tm-u-7", Platform: "openai", Status: "active", Schedulable: true, RateMultiplier: 0.8, Concurrency: 2},
		{ID: 99, Name: "tm-u-orphan", Platform: "anthropic", Status: "active"},
	}
	got := fuseAccounts(ups, engines)
	if len(got) != 2 {
		t.Fatalf("len %d", len(got))
	}
	if !got[0].Linked || got[0].LinkState != linkLinked || got[0].EngineName != "tm-u-7" || got[0].MarketStatus != "listed" {
		t.Fatalf("linked row %+v", got[0])
	}
	if got[1].Linked || got[1].LinkState != linkEngineOnly || got[1].MarketStatus != "仅引擎" || got[1].EngineName != "tm-u-orphan" {
		t.Fatalf("orphan %+v", got[1])
	}
}

func TestFuseAccountsMarksMissingEngine(t *testing.T) {
	got := fuseAccounts([]listing.Upstream{{ID: 1, Platform: "openai", Status: "ready", Sub2APIAccountID: i64(5)}}, nil)
	if got[0].Linked || got[0].EngineStatus != "未同步" || got[0].LinkState != linkOrphan {
		t.Fatalf("%+v", got[0])
	}
}

func TestFuseGroupsJoinsPoolToEngine(t *testing.T) {
	pools := []scheduling.Pool{{ID: 2, Name: "tm-pool-gpt", Platform: "openai", Sub2APIGroupID: i64(8), SellMultiplier: 0.7, Enabled: true}}
	groups := []replica.Group{{ID: 8, Name: "tm-pool-gpt", Platform: "openai", RateMultiplier: 0.7}}
	got := fuseGroups(pools, groups)
	if len(got) != 1 || !got[0].Linked || got[0].LinkState != linkLinked || got[0].EngineName != "tm-pool-gpt" || got[0].PoolID != 2 {
		t.Fatalf("%+v", got)
	}
}
