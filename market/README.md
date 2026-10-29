# market — 闲猫闲置 TOKEN 市场层

本目录是闲猫产品的市场能力：挂单、Edge、质检、结算、C 端前后台等。

## 原则

1. **仓库根目录** `backend/`、`frontend/`、`deploy/` 为网关与管理端；本目录为市场增量。
2. **C 端 UI 用 `market/web`**，保持闲置 TOKEN 市场 / 买家 / 卖家体验。
3. **管理端市场运营**在 `frontend/` 侧栏「闲猫」增量中（页面源码在 `frontend/src/market-admin/`），共用管理员会话。
4. 模块地图与 ADR：`docs/dev/consolidation-module-map.md`、`docs/adr/0001-sub2api-market-consolidation.md`。
5. 部署约定见 [DEPLOY.md](DEPLOY.md)；本地单端口见 [TEST-LOCAL.md](TEST-LOCAL.md)。
6. 协作与 agent 须知见仓库根的 [AGENTS.md](../AGENTS.md)。

## 目录

```
market/
├── cmd/          # api / worker / edge
├── internal/     # listing / buyer / edge / billing / …
├── web/          # C 端主前端（src/views/admin 为未接入路由的旧控制台，勿在此改后台功能）
├── migrations/   # 市场库迁移
└── docs/         # 市场侧文档
```

## 本地验证

```bash
# 仓库根
cd backend && go build -o /dev/null ./cmd/server
cd ../market && scripts/dev.sh up
```
