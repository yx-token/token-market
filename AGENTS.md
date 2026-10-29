# 协作须知（给协作者与 AI agent）

本文件只记**从代码里不容易看出来、已经踩过坑**的事实。新增条目请写明证据和自查方法，并注明核实日期。

## 1. 改后台页面：以 `frontend/src/market-admin/` 为准

闲猫运营后台（检测策略、第三方检测站、厂商、保障池、题库、上游、挂单、资金等）**线上渲染的是 `frontend/src/market-admin/*.vue`**：
路由 `/admin/market/*`，接口 `/api/v1/admin/market/*`，随管理端一起构建到 `market/engine-dist`（见 `market/Dockerfile` 的 `admin-web` 阶段）。

`market/web` 是 C 端（市场、买家中心、卖家中心）。它里面有一批**未接入路由的旧控制台文件**：

- `market/web/src/views/admin/` 下除 `OpenEngineView.vue`（`/open-admin` 跳转页，在用）以外的全部页面；
- `market/web/src/layouts/ConsoleLayout.vue`，以及 `components/console/*`、`stores/consoleTabs.ts` 等同样不可达的配套文件。

它们不进构建产物，只有 `market/web/src/__tests__/` 在引用。**在这里改后台功能，单测会通过，线上不会有任何变化**（2026-10-29 一次检测策略修复就改错了这里，浏览器验收时才发现）。

- 不确定某个页面是否在线上：在 `market/web/dist` 里搜它的独有文案；或以 `market/web/src/main.ts` 为入口做 import 可达性分析。
- 改完后台页面，必须在真实后台里点一遍，不能只看 vitest。本地起环境见 `market/TEST-LOCAL.md`；端口被别的项目占用时，用 `market/scripts/dev.sh` 里的 `DEV_*` 变量覆盖。
- 这批旧文件是否删除尚未决定。决定前保留，不要继续往里加功能。

核实于 2026-10-29：`views/admin` 下 27 个 `.vue` 中只有 `OpenEngineView.vue` 能从 `main.ts` 到达。

## 2. 引擎侧 `/api/v1/admin/market/*`：native 简表会让编辑弹窗丢配置

引擎进程（`backend/`）对这个前缀有两种实现，见 `backend/internal/server/routes/market.go`：

- **native**：`registerNativeIdleCat` 里的 `bind(...)`。引擎自己用简化 SQL 查市场库（`backend/internal/idlecat/lists.go`），只返回少数几列。
- **relay（转发）**：`registerIdleCatRelay` 清单里的接口。引擎在进程内直接调用 market 原来的管理处理函数（`enginehost.ServeAdmin`），响应原样返回，字段与校验和 market 自己的接口一致。

编辑弹窗要的字段如果走 native 简表，**回显会是空的，管理员保存时又会用空值覆盖库里的配置**。

- 检测策略、第三方检测站的读取已改走 relay。
- 厂商、保障池、题库、上游、质量概览、市场分类：native 返回与 market 侧返回的字段不一致，**尚未处理**。动这些页面的编辑功能前，先对比两侧返回的字段。
- 详情与证据：`market/docs/2026-10-29-detection-policy-and-thirdparty-audit.md` §二。
