package idlecat

import (
	"context"
	"os"
	"testing"
)

func TestStoreAgainstLocalDemo(t *testing.T) {
	dsn := os.Getenv("MARKET_DATABASE_DSN")
	if dsn == "" {
		dsn = "postgres://tm:tm@127.0.0.1:55432/tm_dev?sslmode=disable"
	}
	engine := os.Getenv("MARKET_ENGINE_DSN")
	if engine == "" {
		engine = "postgres://tm:tm@127.0.0.1:55432/tm_dev_sub2api?sslmode=disable"
	}
	_ = os.Setenv("MARKET_DATABASE_DSN", dsn)
	_ = os.Setenv("MARKET_ENGINE_DSN", engine)
	s, err := OpenFromEnv()
	if err != nil {
		t.Skip("demo db unavailable:", err)
	}
	defer s.Close()
	ctx := context.Background()
	st, err := s.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Sub2APIVersion == "" {
		t.Fatal("empty version")
	}
	vs, err := s.ListVendors(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) == 0 {
		t.Fatal("no vendors")
	}
	acc, err := s.ListSupplyAccounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(acc) == 0 {
		t.Fatal("no supply accounts")
	}
	gr, err := s.ListSupplyGroups(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(gr) == 0 {
		t.Fatal("no supply groups")
	}
	t.Logf("status=%s vendors=%d accounts=%d groups=%d", st.GuardState, len(vs), len(acc), len(gr))
}
