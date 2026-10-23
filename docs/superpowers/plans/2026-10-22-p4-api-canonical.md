# P4 管理端市场 API 收敛 Implementation Plan

**Goal:** Drop IdleCat bridge route aliases; document canonical `/api/v1/admin/market/*` paths used by market-admin.

**Done:**
- [x] Remove alias GET binds in `backend/internal/server/routes/market.go`
- [x] `docs/dev/admin-market-api-canonical.md`
- [x] `market-admin` views use `MARKET_API_PREFIX` (no `/api/admin/*` literals)
- [x] payments GET via relay; drop idlecat `ListMerchants`/`PaymentStatus`; PaymentsView normalizes map→rows
- [x] settings + market-discovery via relay; drop idlecat platform_settings / PutSettings / PutDiscovery duplicate
