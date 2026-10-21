package idlecat

import "testing"

// 市场库晚于引擎启动时仍返回 Store，闲猫路由照常挂载，库恢复后请求自动成功。
func TestOpenFromEnvToleratesUnreachableDatabase(t *testing.T) {
	t.Setenv("MARKET_DATABASE_DSN", "postgres://market:x@127.0.0.1:1/token_market?sslmode=disable&connect_timeout=1")
	t.Setenv("MARKET_ENGINE_DSN", "postgres://sub2api:x@127.0.0.1:1/sub2api?sslmode=disable&connect_timeout=1")
	s, err := OpenFromEnv()
	if err != nil {
		t.Fatalf("unreachable database should not be fatal: %v", err)
	}
	defer s.Close()
	if s.Market == nil || s.Engine == nil {
		t.Fatalf("both handles should be kept for lazy reconnect: %+v", s)
	}
}

func TestOpenFromEnvRequiresMarketDSN(t *testing.T) {
	t.Setenv("MARKET_DATABASE_DSN", "")
	if _, err := OpenFromEnv(); err == nil {
		t.Fatal("empty MARKET_DATABASE_DSN should be rejected")
	}
}
