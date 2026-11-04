# 引擎代理的闲猫共享配置与验收

核实日期：2026-11-04。代码未提交，未修改版本，未部署。

## 实现与取舍

共享配置放在引擎原有 `/admin/proxies` 页。新增、编辑有「闲猫共享」开关，列表有标识，包含中英文文案。闲猫 `/admin/market/proxies` 页仅显示卖家自有代理，移除平台代理创建和共享操作。

没有修改 sub2api 的 ent schema 或生成代码。市场新增 `engine_proxy_sharing`，以引擎代理 ID 关联共享标记、检测结果、检测时间和检测时的引擎 `updated_at`。每项映射复用一个市场 `proxies` 行，继续使用原有 `upstreams.proxy_id` 和绑定触发器。新建映射只保存内部占位地址，不复制引擎代理地址、认证信息或密码。这比扩展引擎 schema 更少影响后续合并上游。

管理端经以下 relay 接口读写：

- `GET /api/v1/admin/market/engine-proxies/sharing`
- `PUT /api/v1/admin/market/engine-proxies/:id/sharing`
- `POST /api/v1/admin/market/engine-proxies/:id/test`

原引擎连通性检测结果保存在 Redis，不作为持久资格来源。开启共享、页面单条/批量连接检测经市场记录结果；worker leader 启动时及每 5 分钟刷新共享代理，每项最多 5 秒。分配只接受 24 小时内最近一次成功的快照。检测失败、HTTP 错误或超时会持久化失败；旧检测结果通过开始时间 CAS 防止覆盖较新的结果。引擎配置 `updated_at` 变动后旧快照失效。

## 分配与保护规则

| 路径 | 结果 |
| --- | --- |
| 必须代理厂商 | 必须选择自己已同步、检测成功的代理；不能借用共享或直连绕过 |
| 非必须厂商，未自选代理 | 引擎共享、启用、未过期、最新检测合格的代理严格轮询 |
| 非必须厂商，自选代理 | 使用卖家自己的已同步、检测成功代理 |
| 非必须厂商，无合格共享代理 | 直接服务器出口，不受开发用 `allow_no_proxy` 限制 |
| 导入文件含代理 | 保持登记卖家代理、检测后绑定的原有语义 |
| reference / bypass | 保持原有独占平台代理和 `allow_no_proxy` 语义，排除引擎映射行 |

共享分配沿用 `shared_proxy_rotation.last_proxy_id`，按市场映射 ID 排序；同一 PostgreSQL advisory transaction lock 覆盖游标读取、选择、绑定和游标推进。游标与绑定一起提交，失败一起回滚。分配持锁期间只读数据库，不调用网络检测。

引擎 PUT、DELETE、batch delete、代理导入和账号导入也使用同一把锁。状态读取、差异判断、活跃市场绑定检查、原生写入处于同一锁区间，避免检查与写入之间的并发窗口。绑定检查复用持锁事务，不额外从市场连接池取连接。仍有活跃市场绑定时，停用、删除、修改地址/凭证/有效期和取消共享返回 409；名称修改和未变化的配置允许保存。引擎原有账号使用中的删除保护保持生效。

卖家接口只返回共享是否可用及数量，不返回引擎代理端点或凭证。市场 admin 代理列表、共享列表在空时返回 `[]`，并保留前端 `marketSend` 对 `{data:null}` 的修复。

## 迁移

新增 `0049_engine_proxy_sharing.sql`：

- 已同步的旧共享平台代理转换为映射，保留原市场代理 ID、活跃上游绑定及共享标记；首次检测前不参与新分配。
- 已映射行的旧 `proxies.is_shared` 清除，资格以后由映射表决定。
- 未同步旧共享记录继续保留；没有多活跃绑定的行清除旧标记，多绑定行保留兼容标记以免迁移破坏旧绑定。这类行不进入新候选集。
- 更新绑定/策略触发器，允许共享映射多绑定；活跃绑定期间拒绝取消共享和破坏性市场代理操作。

线上市场代理表原为 0 行，不需要人工数据清理。已有本地多绑定数据的迁移有真实 PostgreSQL 回归测试。

## 验证结果

最终真实数据库命令使用独立 PostgreSQL 55480、Redis 56480：

```sh
cd market
TM_TEST_DATABASE_DSN='postgres://tm:tm@127.0.0.1:55480/tm?sslmode=disable' \
TM_SUB2API_REPO=/Users/duegin/project/token-market \
TM_TEST_REDIS_ADDR=127.0.0.1:56480 \
go test -tags integration ./internal/listing ./internal/platform/db ./internal/httpapi ./internal/app ./internal/sub2api/adminclient ./internal/workerloop ./internal/e2e -count=1
```

通过：listing 12.634s、db 4.455s、e2e 62.991s，其余受影响包通过。覆盖引擎标共享命中、18 路严格并发轮询、分配零网络、失效/过期/删除/配置改变/检测失败/过旧快照剔除、无候选直连、必须代理拒绝、旧多绑定迁移、使用中保护、检测结果并发更新、空列表。e2e 保留两代理连续六轮精确断言，以及无共享时直连授权、检测到上架流程。早先一次整包运行的 `TestProbeTimeouts` 时序失败未改实现，随后完整组及独立 `-count=3` 均通过。

普通 Go 测试和 vet 通过：market enginehost/listing/httpapi/app/adminclient/workerloop；backend `./internal/server/... ./cmd/server`，vet routes/admin handler。backend 代理路由定向 `-race` 通过。

golangci-lint 使用 `/tmp/tm-gobin/golangci-lint` v2.15.0（Go 1.27.3），以下三组最终均为 `0 issues`，日志 `/tmp/tm-engine-share-*-lint-final.log`：

```sh
cd market
/tmp/tm-gobin/golangci-lint run --allow-parallel-runners ./...
/tmp/tm-gobin/golangci-lint run --allow-parallel-runners --build-tags integration,smoke ./...
cd ../backend
/tmp/tm-gobin/golangci-lint run --timeout=30m --allow-parallel-runners ./...
```

两端 `pnpm typecheck` 通过。market/web 全量 35 文件、312 测试通过；frontend 本次相关 4 文件、23 测试通过，连接测试额外独立重复 20 次通过。frontend 最终全量 374 文件通过、6 文件失败；2904 测试通过、12 失败，均为既有未修改的测试：settings.authSourceDefaults、openaiFastPolicyLocales、feature-access、UseKeyModal、SubscriptionPlanCard、stripeLazyLoading。日志 `/tmp/tm-engine-sharing-frontend-final-tests.log`。

## 真实环境证据

依 `TEST-LOCAL.md` 用 dev.sh 启动真实嵌入引擎，隔离站点 18090、Edge 18091、引擎 18280、第三方替身 18282、数据目录 `/tmp/tm-proxy-data`。使用 ego-browser 的单一 TaskSpace 9，验收后已关闭。旧 tm-pay 的 8080/8090、容器和数据目录未改动。

- 浏览器创建 `engine-share-A/B` 并勾选共享成功，重新编辑开关回显为 true。
- 闲猫卖家代理 0 行，正常显示「暂无代理」，没有 `.filter` 崩溃。截图 `/tmp/tm-engine-share-empty-admin.png`。
- 必须代理的 Kimi 未选自有代理，点击下一步后停留代理步骤并显示提示。截图 `/tmp/tm-engine-share-required.png`。
- 非必须 Claude 默认显示「平台共享代理（自动分配）」。截图 `/tmp/tm-engine-share-seller-option.png`。
- 浏览器连续建立上游 #7/#8/#9/#10，对应市场代理 19/20/19/20，即 A/B/A/B。
- 使用中取消共享明确显示错误，截图 `/tmp/tm-engine-share-in-use.png`。
- 解绑后在浏览器取消 A 共享，后续创建只选 B。随后取消 B 后无候选直连。
- 最终二进制接口复验 A/B/A/B；使用中停用、删除、取消共享均 409；解绑后取消 A 并停用 B，后续发布直连；空管理列表 `[]`；必须代理厂商无代理 400。日志 `/tmp/tm-engine-share-final-api-results.log`。
- 卖家详情响应检查没有地址、端口、引擎 ID、用户名或密码。

5 路并发真实发布接口均 201，耗时 47.52、51.63、58.29、65.52、70.09ms，均值 58.61ms，最大 70.09ms。记录 `/tmp/tm-engine-share-concurrency.json`。这是本机已持久化成功检测快照下的发布耗时，不含管理员开启共享的外网检测。

最终执行 dev.sh down，18090/18091/18280/18282 已无监听，本次 18311/18312 替身已结束；旧 8080/8090 仍为原进程。

## 边界与后续维护

代理名称等编辑也会更新引擎 `updated_at`，因此保守地使检测快照失效；管理员可点击检测立即恢复，或等后台约 5 分钟刷新。自然到期的代理立即退出新分配，但已有上游可能受代理自然到期影响；本次未改变引擎原有到期回退规则。直接修改数据库不走管理 API 的操作不享受应用层共锁保护。

共享代理检测是定期快照，检测后到下一轮之间发生的网络故障不能提前获知；最近一次失败和过期快照均不会继续分配。没有可用候选时按产品规则直连。

## 改动文件

市场核心：`market/internal/listing/engine_proxy_sharing.go`、`onboarding.go`、`proxies.go`；接线：`market/internal/app/app.go`、`market/internal/workerloop/run.go`；接口：`market/internal/httpapi/handlers_admin.go`、`server.go`、`market/internal/sub2api/adminclient/methods.go`、`market/contract/contract.yaml`；迁移：`market/migrations/0049_engine_proxy_sharing.sql`。

后端保护：`market/enginehost/proxy_guard.go`、`backend/internal/handler/admin/proxy_market_guard.go`、`backend/internal/server/routes/proxy_market_guard.go`、`admin.go`、`market.go`。

前端：`frontend/src/views/admin/ProxiesView.vue`、`frontend/src/market-admin/ProxiesView.vue`、`frontend/src/market-admin/api.ts`、`frontend/src/i18n/locales/en/admin/resources.ts`、`frontend/src/i18n/locales/zh/admin/resources.ts`。总控先前的 api.ts 空值修复保留。

测试：backend `proxy_market_guard_test.go`、`market_pools_relay_test.go`；market listing `engine_proxy_sharing_integration_test.go`、`proxy_sharing_integration_test.go`、`shared_platform_proxy_integration_test.go`、`proxies_test.go`；db `engine_proxy_sharing_integration_test.go`；e2e `seller_proxies_integration_test.go`；frontend 两处代理页测试及 `market-admin/__tests__/api.spec.ts`（保留总控原测试并扩充契约覆盖）。
