# market — 闲置算力交易所（叠加在 sub2api 之上）

本目录是 **token-market** 业务在 sub2api 宿主上的落点。

## 原则

1. **仓库根目录的 `backend/`、`frontend/`、`deploy/` 等为完整社区 sub2api**，保持可独立构建与运行；升级社区版本时优先整树合并。
2. **只在 `market/` 增量添加** 交易所能力（挂单、Edge、质检、结算、市场前后台等），默认不修改社区内核文件。
3. **产品 UI 用 `market/web`**（从 token-market/web 迁入），保留现有买家/卖家/运营后台视觉与交互；**不用社区 `frontend/` 换皮**。社区前端可随上游保留，供对照或运维，不是本产品主界面。
4. 过渡期仍可通过 Admin HTTP / 出站队列 / 影子用户驱动 `backend`；进程内调用属于后续可选收敛。
5. 源码迁入顺序与模块地图见 token-market 仓：`docs/dev/consolidation-module-map.md`、`docs/adr/0001-sub2api-market-consolidation.md`。
6. 部署双前端约定见 [DEPLOY.md](DEPLOY.md)；过渡仓退役条件见实施计划「退役条件」一节。

## 目录规划（随迁移填充）

```
market/
├── README.md          # 本说明
├── cmd/               # api / worker / edge（从 token-market/cmd 迁入）
├── internal/          # listing / buyer / edge / billing / …（分批迁入）
├── web/               # 本产品主前端（风格保持 token-market；非社区 frontend）
├── migrations/        # 市场库迁移
└── docs/              # 市场侧文档副本或链接
```

## 基线

- sub2api 标签：**v0.2.14**（当前分支 `market/consolidation`）
- 上游：`https://github.com/Wei-Shaw/sub2api`

## 本地验证社区树仍可用

```bash
# 在仓库根目录
cd backend && go build -o /dev/null ./cmd/server
# 前端按社区 DEV_GUIDE / README 构建
```
