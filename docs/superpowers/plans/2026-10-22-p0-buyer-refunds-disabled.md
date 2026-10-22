# P0 买家侧关闭退款 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enforce product policy「买家侧完全不能退款」by rejecting all buyer refund creation paths and removing buyer refund UI/copy.

**Architecture:** Hard-forbid at billing/buyer service entry points with `BUYER_REFUNDS_DISABLED`; admin「代买家退款」走同一入口一并禁止。保留已有退款单的查询/打款完结供历史单据。Edge/引擎调度不在本阶段改动。

**Tech Stack:** Go (`market/internal/billing`, `market/internal/buyer`, `market/internal/httpapi`), Vue (`market/web`)

**Spec:** `docs/superpowers/specs/2026-10-22-c2c-engine-edge-fusion-design.md`

---

### Task 1: Billing deny helper + gate refund entry points

**Files:**
- Create: `market/internal/billing/buyer_refunds_policy.go`
- Create: `market/internal/billing/buyer_refunds_policy_test.go`
- Modify: `market/internal/billing/funds.go` (`RequestRefund`)
- Modify: `market/internal/billing/order_refunds.go` (`RefundRechargeOrder`)
- Modify: `market/internal/billing/store_credits.go` (`RefundCreditPack`)
- Modify: `market/internal/buyer/packs.go` (`RefundPack`)

- [x] **Step 1: Add policy helper + unit test**
- [x] **Step 2: Call deny at start of each refund creation method**
- [x] **Step 3: `go test` billing + buyer packages for policy test**

### Task 2: Hide buyer wallet refund UI and fix listing copy

**Files:**
- Modify: `market/web/src/views/buyer/WalletView.vue`
- Modify: `market/web/src/views/market/ListingView.vue`
- Modify: `market/web/src/views/admin/FundsView.vue`
- Modify: `market/web/src/views/buyer/KeysView.vue`
- Modify: `market/web/src/views/buyer/StoreCreditsView.vue`

- [x] **Step 1: Remove buyer refund buttons/dialogs from WalletView**
- [x] **Step 2: Remove「退货按卖家服务标签」copy from ListingView**
- [x] **Step 3: Remove admin「代充值退款」UI；Keys/StoreCredits 退货入口**

### Task 3: Commit

- [ ] Commit with message referencing design policy
