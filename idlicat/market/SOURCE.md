# 代码与环境来源

| 内容 | 来源 |
|---|---|
| 社区引擎全树 | https://github.com/Wei-Shaw/sub2api |
| 业务实现（迁入前权威） | 兄弟仓 `../token-market` |
| 部署 / 脚本 / Dockerfile / Makefile | 已从 token-market 拷入 |
| CI / Release | `.github/workflows/market-*.yml` |
| Go module | `github.com/yx-token/idlicat/market` |

## 已迁入清单

| 模块 | 状态 | 备注 |
|---|---|---|
| 社区全树 | 在仓 | 勿裁剪 |
| 市场环境 deploy/scripts/CI | 已拷入 | |
| `market/internal/*` 业务包 | 已迁 | 含 platform/sub2api/listing/scheduling/buyer/edge/quality/billing/httpapi/app 等 |
| `market/internal/testutil` | 已迁 | 含 fakes |
| `market/contract` `migrations` | 已迁 | |
| `market/cmd/*` | 已迁 | api/worker/edge/market-runner/s2a-compat/fakes；`go build` 通过 |
| `market/web` | 已迁 | 风格原样；`pnpm test`/`pnpm build` 通过 |
| `config.example.yaml` | 在仓根 | 供 config 单测相对路径 |
| `market/internal/e2e` | 已迁 | integration 标签可编译 |

## 退役检查

见实施计划「过渡仓退役条件」与「功能全覆盖清单」。
