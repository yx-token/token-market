package billing

import "github.com/yx-token/token-market/market/internal/platform/httpx"

// DenyBuyerRefund 落实产品政策：买家侧完全不能退款（含管理员代买家发起）。
// 见 docs/superpowers/specs/2026-10-22-c2c-engine-edge-fusion-design.md
func DenyBuyerRefund() error {
	return httpx.Forbidden("BUYER_REFUNDS_DISABLED", "买家侧不支持退款")
}
