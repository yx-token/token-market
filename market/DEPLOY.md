# 部署约定（管理端 = sub2api · C 端 = 闲置 TOKEN）

## 单端口宿主（推荐）

同一 `market-api` 进程对外一个 HTTP 端口：

| 路径 | 产物 |
|---|---|
| `/` | C 端 `market/web` → `TM_HTTP_WEB_DIR` |
| `/admin`、`/admin-assets` | 管理端 `frontend`（`pnpm run build:market-host`）→ `TM_HTTP_ADMIN_WEB_DIR` |
| `/api/*` | 市场控制面 API |
| Edge 另端口 | 买家模型流量（非浏览器站点） |

- 管理端登录：`/admin/login`（不与 C 端 `/login` 冲突）
- 管理端运营：`/admin/market`

示例：`https://www.example.com` → 上述全部；`https://edge.example.com` → Edge。

## 管理端

- 代码：改造后的社区 `frontend/`（闲猫 console 风格）
- 市场运营 API：同宿主 `/api/admin/*`；终态鉴权统一

## C 端（闲置 TOKEN）

| 组件 | 说明 |
|---|---|
| `market/web` | 市场 / 买家 / 卖家 |
| `market` API + Edge | 控制面与买家模型流量 |

## 禁止

- 把社区默认皮肤当作终态管理端
- 用社区用户门户替换 C 端闲置 TOKEN
- 长期保留「社区壳 + 独立市场登录墙」双后台
