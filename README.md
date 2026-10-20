# 闲猫 · IdleCat

闲置 TOKEN 交易所与运营控制台。卖家挂出订阅额度，买家按倍率购买与调用；管理端统一在一套后台完成供给、质检、资金与运营。

## 产品形态

| 面 | 路径 | 说明 |
|---|---|---|
| **C 端市场** | `/`（`market/web`） | 市场货架、买家、卖家 |
| **运营管理端** | `/admin`（`frontend/`） | 网关、账号、分组；侧栏「闲猫」为市场增量 |
| **市场 API** | `market/` | 挂单、Edge、质检、人民币账本等 |

## 本地单端口

```bash
cd market && scripts/dev.sh reset && scripts/dev.sh up
```

- C 端：http://127.0.0.1:8080/
- 管理端：首页「控制台」→ `/admin/dashboard`

更多说明见 [`MARKET.md`](MARKET.md)、[`docs/adr/0001-sub2api-market-consolidation.md`](docs/adr/0001-sub2api-market-consolidation.md)、[`market/TEST-LOCAL.md`](market/TEST-LOCAL.md)。

## 技术栈

Go · PostgreSQL · Redis · Vue 3

## 仓库结构

```
backend/    # 网关与管理 API
frontend/   # 管理端 SPA
market/     # 市场后端 + C 端 web
docs/       # 产品 ADR 与并仓文档
```
