# P2 处置互斥 + 保障池回收优先 Implementation Plan

**Goal:** Enforce A/B vs buyback mutual exclusion messaging; advertise `pool-buyback-first` when routes include a pool hop (engine already ranks buyback accounts).

**Done:**
- [x] `DISPOSITION_ACTIVE` via `requireNoOpenDisposition` + unique-index mapping
- [x] `SelectSchedulePolicy` / `BuildSchedulePlan` policy wiring
- [x] Contract doc update
- [x] Unit tests
