# 卖家代理一对多

核实日期：2026-10-30。

## 数据模型

`upstreams.proxy_id` 是绑定关系的唯一来源。卖家自有代理（`proxies.seller_id IS NOT NULL`）可关联任意数量的同卖家账号；平台代理继续独占。

代理 API 新增 `assigned_upstream_ids`（升序数组，无绑定时为 `[]`）和 `assigned_upstream_count`。绑定包括所有 `status <> 'removed'` 的账号，含草稿、失败及移除中的账号。旧 `assigned_upstream_id` 保留为最小绑定 ID 的兼容投影；业务判断不再依赖它。保留该列可让旧消费者继续读取，同时避免建立第二套关系表。

`status = 'assigned'` 表示至少有一个未移除账号；对卖家代理不表示禁止继续绑定。最后一个账号移除后变 `available`。禁用代理不能新增绑定；编辑、启停和删除都要求没有未移除账号。

## 迁移 0043

1. 锁定代理和上游表，先将旧 `assigned_upstream_id` 指向但未填写 `proxy_id` 的有效绑定回填。
2. 如旧字段与 `upstreams.proxy_id` 指向不同代理，或平台代理已有多个活跃账号，报错并回滚，要求先核对数据，避免悄悄覆盖关系。
3. 清除已 `removed` 账号的 `proxy_id`，解除无效占用及删除代理时的历史外键阻碍。
4. 移除 `proxies_assigned_upstream_id_key` 和 `upstreams_one_proxy_idx`，增加活跃绑定查询索引。
5. 从上游关系重算代理状态和旧字段；安装数据库触发器，在关系或账号状态变化时维护投影。

触发器按代理 ID 顺序锁行，校验代理归属、禁用状态和平台代理独占。应用的代理修改接口先锁同一代理行，再用独立语句重读绑定，避免等待锁期间绑定发生变化却仍使用旧快照。删除代理在数据库删除提交后才尝试清理引擎记录。

上游进入 `removed` 时，触发器仅清除该账号的代理关系。其余未移除账号仍计入占用。`Runner.completeRemoval` 只调整原释放 SQL，调度轮次和删除宽限期不变。

## 代码入口

- `internal/listing/onboarding.go`：允许选择已占用的自有代理；平台代理自动分配仍要求无绑定。
- `internal/listing/account_import.go`：导入同一卖家的多个账号时可复用同一代理。
- `internal/listing/proxies.go`：派生列表字段，编辑、启停和删除使用事务锁及活跃绑定保护。
- `../backend/internal/idlecat/lists.go`、`writes.go`：引擎 native 代理列表和启停行为与市场侧对齐。
- `web/src/components/OnboardingWizard.vue`、`ProxyInventory.vue`、`web/src/views/seller/PublishView.vue`：选择已绑定代理、展示全部账号、按绑定数保护操作。
- `web/src/views/seller/ProxiesView.vue`：同步修正自有代理可共享的页面说明。
- `../frontend/src/market-admin/ProxiesView.vue`：真实后台的绑定筛选、统计、账号展示和停用保护。

## 验证

真实 PostgreSQL 16，测试 DSN 指向本地专用容器；每个集成测试建立独立临时数据库。覆盖存量迁移、共享接入、完整导入、归属隔离、平台独占、并发绑定、锁等待后重检、逐个移除及调度释放。

执行通过：

```sh
# market
go build ./...
TM_TEST_DATABASE_DSN='postgres://tm:tm@127.0.0.1:55432/tm?sslmode=disable' go test -tags=integration ./internal/listing ./internal/scheduling ./internal/platform/db -count=1
TM_TEST_DATABASE_DSN='postgres://tm:tm@127.0.0.1:55432/tm?sslmode=disable' go test -race -tags=integration ./internal/listing ./internal/scheduling ./internal/platform/db -count=1

# backend
go build ./...
MARKET_ENGINE_DSN='postgres://tm:tm@127.0.0.1:55432/tm_sub2api_live?sslmode=disable' TM_TEST_DATABASE_DSN='postgres://tm:tm@127.0.0.1:55432/tm?sslmode=disable' go test -tags=integration ./internal/idlecat -count=1 -v

# market/web
pnpm test src/__tests__/onboarding.spec.ts src/__tests__/proxy-diagnostics.spec.ts src/__tests__/publish-models.spec.ts src/__tests__/proxy-bindings.spec.ts
pnpm typecheck

# frontend
pnpm test:run src/market-admin/__tests__/ProxiesView.spec.ts
pnpm typecheck
```

前端目标测试分别 33 和 2 项通过。开发脚本完成两个前端生产构建，并启动真实本地引擎。使用 ego-browser 在 `/admin/market/proxies` 验证同一代理显示两个账号，停用按钮禁用，已绑定和未绑定筛选正确，统计含两个账号。卖家发布页成功选择已绑定两个账号的代理；卖家代理列表展示两个可点击账号；实际删除一个草稿上游后仍显示一个绑定，代理继续占用并保持操作保护；删除最后一个后代理变空闲，编辑、停用和删除恢复可用。

首轮集成失败及处理：

```text
migrated seller proxy cannot be shared: ERROR: duplicate key value violates unique constraint "upstreams_one_proxy_idx" (SQLSTATE 23505)
```

原因是 `0012` 还建立了活跃绑定唯一索引；已纳入新迁移移除，复跑通过。

native 全包首次执行因旧 demo 测试默认寻找替身引擎库而失败：

```text
--- FAIL: TestStoreAgainstLocalDemo (0.02s)
    store_integration_test.go:45: pq: database "tm_dev_sub2api" does not exist
```

显式指定真实本地引擎库 `tm_sub2api_live` 后，全包通过，demo 测试也通过。

## 发布与回退

部署时运行内嵌迁移并同时更新市场及引擎服务和两个前端。已有共享数据不能直接恢复活跃绑定唯一索引；回退到一对一前需逐一解除多余绑定。未操作生产环境。
