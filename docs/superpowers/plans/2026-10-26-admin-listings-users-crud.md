# Admin Listings + Users CRUD Implementation Plan

> Spec: `docs/superpowers/specs/2026-10-26-admin-listings-users-crud-design.md`

## Files

| File | Role |
|---|---|
| `market/internal/listing/admin_listings.go` | Admin list/get/create/update/force actions |
| `market/internal/account/admin_users.go` | Admin list/get/create/update user |
| `market/internal/httpapi/handlers_admin_listings.go` | HTTP handlers |
| `market/internal/httpapi/handlers_admin_users_ext.go` | HTTP handlers for users CRUD |
| `market/internal/httpapi/server.go` | Register routes |
| `backend/internal/server/routes/market.go` | Relay paths |
| `frontend/src/market-admin/ListingsView.vue` | UI |
| `frontend/src/market-admin/UsersView.vue` | Expand |
| `frontend/src/router/index.ts` + AppSidebar + i18n | Wire |

## Tasks

1. Listing admin service + handlers + tests
2. User admin service + handlers
3. Relay + frontend ListingsView
4. Expand UsersView
5. typecheck / i18n
