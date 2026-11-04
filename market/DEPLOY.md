# 部署约定（管理端 = sub2api · C 端 = 闲置 TOKEN）

## 单端口宿主（推荐）

嵌入模式下同一宿主主端口承载站点与买家模型调用。带 `tm-` 的请求在 sub2api 网关鉴权前交给共享 Edge handler；普通 sub2api Key 仍进入原网关。8090 可继续保留为旧客户端兼容入口。

分流范围与 Edge 放行白名单分别维护：主端口把买家网关请求交给 Edge，Edge 继续按原有白名单放行或拒绝，两个入口的接口能力一致。8090 监听失败只记录错误，不停用主端口共享 handler。

| 路径 | 产物 |
|---|---|
| `/` | C 端 `market/web` → `TM_HTTP_WEB_DIR` |
| `/admin`、`/admin-assets` | 宿主管理端 `frontend` 构建产物 |
| `/api/*` | 市场控制面 API |
| `/api/v1/*` | 宿主 API；市场运营为 `/api/v1/admin/market/*` |
| `/v1/*`、`/v1beta/*`、`/antigravity/*`、`/backend-api/*`、模型别名 | `tm-` 请求由 Edge 处理，其他 Key 仍由宿主网关处理 |

- 管理端登录：`/admin/login`（不与 C 端 `/login` 冲突）
- 管理端运营：`/admin/market`
- `MARKET_EMBED=1` 时 `EDGE_PUBLIC_URL` 应设为站点公网 HTTPS origin（例如 `https://idlicat.com`），不要带 `:8090`；C 端可在该值为空时回退当前 origin，但第三方检测 worker 需要此服务端地址，且要求公网可达 HTTPS。
- `.env.example` 中 `EDGE_PUBLIC_URL` 仅提供注释示例，部署时按真实域名填写。`EDGE_TRUSTED_PROXIES` 默认为空；需要采信 nginx 的转发地址时，显式填写实际直连代理 IP/CIDR（传入 `TM_EDGE_TRUSTED_PROXIES`）。
- 反向代理将站点模型路径转发到宿主主端口，关闭 response/request buffering，并为长连接配置足够的读写超时；Edge handler 会 flush SSE 响应并应用自身 body 限制与首字节超时。

示例：`https://www.example.com` → 站点与 `tm-` Edge；旧客户端可暂时继续使用独立反代的 `https://edge.example.com`。

独立进程模式仍由 `market-edge` 监听自己的端口，并通过 `EDGE_PUBLIC_URL` 公布 Edge 域名或带端口的 URL，不会把流量自动挂到独立 `market-api`。部署配置见 `market/deploy/.env.example`；主端口共享入口仅适用于 `MARKET_EMBED=1` 且未关闭 `MARKET_EMBED_EDGE`。

CC Switch 的买家 `GET /v1/usage` 由 Edge 本地读取市场余额，不会转发内部 Key。
独立 `market-edge` 使用此功能需配置市场库的 `TM_DATABASE_DSN`；未配置或库不可用时该接口返回 503。
金额口径、兼容路径及验收步骤见 [买家余额查询说明](docs/2026-11-04-ccswitch-buyer-balance.md)。

## 管理端

- 代码：改造后的社区 `frontend/`（闲猫 console 风格）
- 市场运营 API：同宿主 `/api/v1/admin/market/*`；终态鉴权统一

## C 端（闲置 TOKEN）

| 组件 | 说明 |
|---|---|
| `market/web` | 市场 / 买家 / 卖家 |
| `market` API + Edge | 控制面与买家模型流量 |

## 禁止

- 把社区默认皮肤当作终态管理端
- 用社区用户门户替换 C 端闲置 TOKEN
- 长期保留「社区壳 + 独立市场登录墙」双后台
