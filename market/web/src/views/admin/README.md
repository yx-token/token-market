# views/admin：遗留旧控制台（未接入路由）

本目录除 `OpenEngineView.vue`（`/open-admin` 跳转页，在用）外，**没有任何页面被路由引用**，不进构建产物，只有 `src/__tests__/` 在引用。

线上运营后台是仓库根的 `frontend/src/market-admin/`（路由 `/admin/market/*`）。**要改后台功能，请改那里**；在这里改，单测会通过，线上不会有任何变化。

这批文件是否删除尚未决定。决定前保留，请不要继续往里加功能。详见仓库根的 `AGENTS.md`。

核实于 2026-10-29：以 `src/main.ts` 为入口做 import 可达性分析，本目录 27 个 `.vue` 中只有 `OpenEngineView.vue` 可达。
