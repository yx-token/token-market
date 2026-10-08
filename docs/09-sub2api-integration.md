# 09 sub2api 集成与兼容闸门

## 1. 适配层

所有与 sub2api 的交互集中在 `internal/sub2api`，业务代码不直接拼 sub2api 的 URL 或 SQL。

| 模块 | 职责 |
|---|---|
| `apiwire` | 公共传输层：解析 sub2api 响应信封（`code` 可能是数字或字符串错误码）、错误分类与可重试判断 |
| `adminclient` | Admin API 客户端，`x-api-key` 认证；创建类调用带 `Idempotency-Key` |
| `userclient` | 影子用户的登录、刷新 token、内部 key 增删改 |
| `replica` | 只读库查询。SQL 只放在 `contract/queries/`（内嵌进二进制），本包只负责执行与扫描 |
| `ops` | `sub2api_ops` 出站表：入队（幂等键去重）、发送器（同一 target 严格按序、退避重试、租约回收、死信） |
| `opshandlers` | 出站操作类型到 Admin API 调用的映射 |
| `compatguard` | 运行时兼容自检，安全模式下关闭出站发送 |
| `contract`（仓库根目录） | 契约文件 `contract.yaml`、只读查询与加载代码 |

## 2. Admin API 调用清单

| 用途 | 接口 |
|---|---|
| 版本 | `GET /api/v1/admin/system/version` |
| 代理 | `POST/PUT/DELETE /api/v1/admin/proxies`、`POST /proxies/:id/quality-check` |
| Claude 授权 | `POST /admin/accounts/generate-auth-url`、`/exchange-code`、`/generate-setup-token-url`、`/exchange-setup-token-code` |
| OpenAI 授权 | `POST /admin/openai/generate-auth-url`、`/exchange-code`、`/create-from-oauth`、`/create-from-codex-pat` |
| Gemini / Antigravity / Grok 授权 | `POST /admin/{gemini,antigravity,grok}/oauth/auth-url`、`/oauth/exchange-code`；Grok `/oauth/create-from-oauth` |
| 账号 | `POST /admin/accounts`、`PUT /admin/accounts/:id`、`POST /admin/accounts/bulk-update`、`POST /:id/schedulable`、`DELETE /:id` |
| 账号信息 | `POST /:id/refresh-tier`、`POST /:id/set-privacy`、`GET /admin/openai/accounts/:id/quota`、`GET /admin/grok/accounts/:id/quota`、`GET /admin/cn-providers/accounts/:id/quota` |
| 分组 | `POST/PUT/DELETE /admin/groups`、`PUT /admin/groups/:id/rate-multipliers`（买家折扣，后续） |
| 用户 | `POST /admin/users`、`PUT /admin/users/:id`（并发、可用分组）、`POST /admin/users/:id/balance`（支持 `Idempotency-Key`）、`GET /:id/balance-history` |

所有路径前缀为 `/api/v1`。具体请求字段实现时以对应版本的 sub2api handler 定义为准，并写进契约。

**幂等性**（对照 sub2api 0.2.14 代码）：

| 接口 | sub2api 的幂等支持 | 本系统的做法 |
|---|---|---|
| 创建账号、创建代理、调整余额 | 支持 `Idempotency-Key`，且**强制要求**（缺少时拒绝） | 用出站记录的幂等键 |
| 创建用户 | 不支持；邮箱唯一 | 重试前按邮箱查询是否已创建 |
| 创建分组 | 不支持 | 重试前按名称（`tm-` 前缀含本系统 ID）查询是否已创建 |
| 创建内部 key | 不支持；`custom_key` 唯一 | 由本系统生成 `custom_key`，重复创建会被拒绝，据此判断已创建 |

**管理员合规确认**：sub2api 要求每个管理员确认《部署与运营合规承诺》后才能调用管理接口，Admin API Key 的调用同样受此约束（未确认返回 HTTP 423，`code` 为字符串 `ADMIN_COMPLIANCE_ACK_REQUIRED`）。确认必须由平台运营方的管理员本人完成，本系统不代为确认。

## 3. 用户 API（影子用户）

sub2api 的 Admin API 不能替用户创建 key，只能用影子用户的登录态调用：

| 用途 | 接口 |
|---|---|
| 登录 | `POST /api/v1/auth/login` |
| 刷新 token | `POST /api/v1/auth/refresh` |
| 内部 key | `POST/PUT/DELETE /api/v1/keys`（`custom_key` 由本系统生成，`group_id` 绑定分组） |

注意事项：

- 登录接口限流每分钟 20 次。影子用户只在创建时登录一次，之后保存 refresh token（加密）续期；刷新失败才重新登录，重新登录走队列。
- sub2api 必须关闭登录验证码，否则无法程序化登录。sub2api 不对公网开放，关闭验证码没有暴露面。

## 4. 只读库查询清单

| 表 | 使用的列 | 用途 |
|---|---|---|
| `usage_logs` | id、request_id、created_at、user_id、api_key_id、account_id、group_id、model、各类 token、total_cost、actual_cost、account_rate_multiplier、first_token_ms、duration_ms、upstream_response_model、upstream_model_mismatch | 用量入账、逐次尝试对账、被动指标 |
| `accounts` | id、name、platform、type、status、error_message、schedulable、priority、concurrency、load_factor、rate_multiplier、rate_limit_reset_at、overload_until、temp_unschedulable_until、temp_unschedulable_reason、extra 中的窗口与配额键、deleted_at | 健康信号、状态对账 |
| `account_groups` | account_id、group_id | 状态对账 |
| `groups` | id、name、platform、rate_multiplier、is_exclusive、利润控制字段 | 状态对账 |
| `users`、`api_keys` | id、email、balance、status；id、user_id、group_id、status | 对账 |
| `schema_migrations` | filename、checksum | 兼容自检 |

不读取 `accounts.credentials`。

## 5. 契约

契约描述本系统依赖 sub2api 的哪些东西，由三部分组成。

### 5.1 读库 SQL

`contract/queries/*.sql` 是本系统用到的全部只读查询，内嵌进二进制，由 `replica` 包执行。兼容检查时把目标版本的迁移执行到空的临时库，再对每条查询执行 `EXPLAIN`：用到的列被删除、改名或类型不兼容，`EXPLAIN` 直接失败。直接用真实 PostgreSQL 校验，比用 sqlc 解析 sub2api 的迁移脚本更可靠（sub2api 的迁移中有 PL/pgSQL、`CONCURRENTLY` 等写法）。

### 5.2 contract.yaml

记录 SQL 表达不了的依赖：

```yaml
sub2api:
  verified_versions: ["0.2.14"]          # 已验证兼容的版本
  migration_fingerprints:                # 每个版本对应的迁移指纹（schema_migrations 文件名集合的哈希）
    "0.2.14": "sha256:…"

jsonb_keys:
  accounts.extra:
    read:  [quota_used, quota_daily_used, session_window_utilization, …]
    write: [window_cost_limit, base_rpm, max_sessions, quota_daily_limit]
  accounts.credentials:
    write: [account_scheduling_threshold, account_mode, api_protocol]

enums:                                   # 新增值视为提示，删除或改名视为冲突
  accounts.platform: [anthropic, openai, gemini, antigravity, grok, kimi, zhipu, minimax, opencode_go]
  accounts.type: [oauth, setup-token, apikey]
  accounts.status: [active, inactive, error, disabled]   # 实现时按 sub2api 实际取值核对

admin_api:                               # 用到的端点与字段
  - method: POST
    path: /api/v1/admin/accounts
    request_fields: [name, platform, type, credentials, extra, proxy_id, concurrency, priority, rate_multiplier, group_ids]
    response_fields: [data.id]
  # …

model_prices:                            # 挂单涉及的模型必须在 sub2api 有官方价
  required: [claude-sonnet-*, claude-opus-*, gpt-5*, kimi-for-coding, glm-5*, …]

config:                                  # 依赖的 sub2api 配置
  gateway.scheduling.prefer_soonest_reset: true
  gateway.usage_record.overflow_policy: sync

response_headers:                        # Edge 对账依赖的响应头
  X-Client-Request-ID: "每个网关请求都返回，usage_logs.request_id = 'client:' + 该值"
```

### 5.3 行为契约测试

用假上游跑场景，验证本系统依赖的行为：

| 行为 | 验证方式 |
|---|---|
| usage_log 记录账号倍率快照 | 账号倍率设为 0.3，发一次请求，断言 `account_rate_multiplier = 0.3` 且 `total_cost` 正确 |
| 分组倍率决定买家扣费 | 分组倍率 1.2，断言 `actual_cost = total_cost × 1.2` |
| 优先级段严格分层 | 两个账号优先级 1 和 50，前者可用时后者不被调度；前者满载时后者接流量 |
| 利润控制排除高倍率账号 | 账号倍率超过阈值时不被调度 |
| 窗口阈值停调度 | 构造窗口用量快照超过 `account_scheduling_threshold`，断言账号不被调度 |
| 失败请求不计费 | 假上游返回 503，断言没有新增 usage_log、余额不变 |
| 组内切换账号只计一次 | 第一个账号失败、第二个成功，断言只有一条 usage_log，且 account_id 为第二个账号 |
| 输出中途上游断开 | 假上游输出一半后断开连接，记录 sub2api 是否产生费用，结果补充到 08 第 6.1 节，便于对账时解释 |
| 输出中途买家断开 | 客户端读到一半后断开，断言 usage_log 按已产生的用量记录 |
| 请求 ID 关联 | 成功和失败的响应都带 `X-Client-Request-ID`；成功请求的 `usage_logs.request_id` 等于 `client:` + 该值 |
| 无可用账号返回 503 | 分组内账号全部移出，断言返回 503 |
| bulk-update 合并 extra | 下发部分键，断言其他键保留 |
| 余额接口幂等 | 同一 `Idempotency-Key` 调两次，余额只增加一次 |
| 自定义 key | `custom_key` 创建的 key 可正常调用 |

**已在 sub2api 0.2.14 上实测**（`internal/sub2api/smoke_test.go`，构建标签 `smoke`）：影子用户登录与刷新令牌；用 `custom_key` 创建、停用、启用、删除 key，重复的 `custom_key` 被拒绝；网关错误响应带 `X-Client-Request-ID`，失败请求没有写 usage_log；余额不足时网关返回 HTTP 403，`code` 为字符串 `INSUFFICIENT_BALANCE`。Admin API 部分需要管理员先完成合规确认才能实测。

## 6. s2a-compat

### 6.1 用法

```bash
# 用生产只读库确定"当前已执行的迁移"，并在空的临时库上校验契约查询
s2a-compat check --sub2api-repo ../sub2api --target v0.2.15 \
  --replica-dsn "$SUB2API_REPLICA_DSN" --scratch-dsn "$SCRATCH_DSN" --out report.md

# 不接触生产库时，用当前版本的 git ref 代替
s2a-compat check --sub2api-repo ../sub2api --current v0.2.14 --target v0.2.15

# 计算某个版本的迁移指纹，写入 contract.yaml 的 verified_versions
s2a-compat fingerprint --sub2api-repo ../sub2api --ref v0.2.15
```

其他参数：`--format md|json`、`--fail-on conflict|warning`（默认只有冲突阻止升级）、`--target-dir`（直接读取迁移目录）、`--contract`（覆盖内嵌契约）。`--scratch-dsn` 必须指向空库，非空时拒绝执行，防止误用生产库。

退出码：0 通过；1 有阻止升级的发现；2 检查本身出错。

### 6.2 检查阶段

| 阶段 | 做什么 | 耗时 |
|---|---|---|
| 0. 静态预检 | 从生产只读库读 `schema_migrations` 得到已执行的迁移；拉取目标版本的 `backend/migrations/`，算出待执行的迁移；扫描其中涉及契约表和列的 `DROP`、`RENAME`、`ALTER … TYPE`、`SET NOT NULL`、约束变更；标出会长时间锁大表（如 `usage_logs`）的迁移 | 秒级 |
| 1. 结构比对 | 第一期先实现轻量版（`--scratch-dsn`）：按 sub2api 迁移执行器的规则把目标版本全部迁移执行到空库，对每条契约查询执行 `EXPLAIN`。完整版：从只读副本导出表结构导入临时库，用目标版本的 sub2api 镜像连接它启动（启动时执行待执行的迁移），再做同样的校验 | 秒级 / 分钟级 |
| 2. 行为契约 | 在同一个临时实例上，接假上游，跑第 5.3 节的全部场景，并按 `admin_api` 校验请求和响应字段 | 分钟级 |

**实现状态（M0）**：阶段 0 与阶段 1 轻量版已实现；阶段 1 完整版与阶段 2 放在 M5，冒烟测试是阶段 2 的雏形。

**静态扫描的识别范围**：`ALTER TABLE` 的删列、改列名、改表名、改列类型、`SET/DROP NOT NULL`、新增列、`CHECK (col IN (…))` 与 `col = ANY (ARRAY[…])` 形式的枚举约束、移动 schema、分区变更；`DROP TABLE`、`TRUNCATE`；大表上非 `CONCURRENTLY` 建索引、大表 `UPDATE`/`DELETE`；PL/pgSQL 代码块（`DO`、函数）同时提到契约表和变更动作时提示人工确认。对 v0.1.136 → v0.2.14 的 107 个迁移试跑：没有误报冲突，11 条人工确认提示（均为触发器函数），48 条新增列提示。

### 6.3 报告

每条结果包含：级别（冲突 / 提示）、位置（表.列 / 接口 / 行为）、旧值 → 新值、触发它的迁移文件、影响本系统的哪个模块。

| 变化 | 级别 |
|---|---|
| 契约用到的列被删除、改名、改类型、变为非空 | 冲突 |
| 契约用到的接口路径或字段被删除、改名 | 冲突 |
| 行为契约测试失败 | 冲突 |
| 枚举新增值、表新增列、新增接口 | 提示 |
| 迁移会长时间锁大表 | 提示（需安排维护窗口） |

## 7. 升级流程

**检查通过**

1. 本系统切换到维护模式：暂停对 sub2api 的写操作（执行器、对账器、余额同步）。
2. 升级 sub2api（固定镜像版本号），等待迁移完成、健康检查通过。
3. 本系统的运行时自检识别到新版本已在 `verified_versions` 中，自动恢复写操作。

**检查有冲突**

1. 闸门拒绝升级，输出报告。
2. 修改本系统适配层，使其**同时兼容当前版本和目标版本**：运行时根据 `schema_migrations` 判断版本，选择对应的查询和字段映射。必须双版本兼容，因为本系统先发布、sub2api 后升级，中间这段时间 sub2api 仍是旧版；sub2api 升级失败回滚时也需要。
3. 把目标版本加入 `verified_versions`，重跑闸门直到通过。
4. 发布本系统。
5. 按"检查通过"的流程升级 sub2api。
6. 稳定运行一段时间后，删除旧版本的兼容代码。

## 8. 运行时自检

market-worker 每 5 分钟读取一次 `schema_migrations` 指纹和 `/admin/system/version`：

- 在 `verified_versions` 中：正常运行。
- 不在其中：进入**安全模式**，停止所有对 sub2api 的写操作，暂停结算入账，告警。买家请求不受影响。

配合以下措施防止绕过闸门：

- 在反向代理层禁用 sub2api 的在线升级和回滚接口（`/api/v1/admin/system/update`、`/rollback`）。
- sub2api 部署固定镜像版本号，只能通过带闸门的流水线修改。

## 9. 示例：0.2.10 → 0.2.14

对 2026-10-08 本地更新的这次升级做静态预检，预期结果：

| 检查项 | 结果 |
|---|---|
| 新增迁移 | `241_add_payment_order_bonus_amount.sql`（支付订单加字段）、`241_add_typesafe_platform.sql`（`user_platform_quotas`、`composite_model_routes` 的平台 CHECK 约束加入 `typesafe`） |
| Ent schema 变更 | `payment_order`、`user_platform_quota` |
| 契约表 | 无变更 |
| Admin 路由 | 仅新增 `POST /admin/accounts/:id/claude/reset-credits/redeem` |
| 结论 | 静态预检、结构比对通过；新平台 `typesafe` 记为提示。行为契约需要实际运行确认 |

对应的契约规则：本系统遇到不认识的平台时按"不支持挂单"处理，不能报错。
