# P1 Edge↔引擎契约 Implementation Plan

> **For agentic workers:** Use executing-plans / subagent-driven-development task-by-task.

**Goal:** Document and code-stabilize Edge as scheduler brain vs engine as execution arm, without changing hop failover behavior yet.

**Architecture:** `BuildSchedulePlan` is the single schedule entry; HTTP/WS loops consume `SchedulePlan`; contract in `docs/dev/edge-engine-contract.md`.

**Tech Stack:** Go `market/internal/edge`

**Spec:** `docs/superpowers/specs/2026-10-22-c2c-engine-edge-fusion-design.md`

---

### Task 1: Contract doc + scheduler types

- [x] `docs/dev/edge-engine-contract.md`
- [x] `market/internal/edge/scheduler.go` + tests
- [x] Package comment update
- [x] Wire HTTP + WS loops; `X-TM-Schedule-Policy` header
