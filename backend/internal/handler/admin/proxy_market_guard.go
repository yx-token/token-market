package admin

import (
	"context"

	"github.com/yx-token/token-market/internal/service"
)

// These readers let native route guards inspect a proxy without exposing the
// admin service or duplicating its repository queries in the market bridge.
func (h *ProxyHandler) GetForMarketMutationGuard(ctx context.Context, id int64) (*service.Proxy, error) {
	return h.adminService.GetProxy(ctx, id)
}

func (h *ProxyHandler) ListForMarketMutationGuard(ctx context.Context) ([]service.Proxy, error) {
	return h.listProxiesFiltered(ctx, "", "", "", "id", "asc")
}
