# 本产品说明（市场叠加层）

本仓库在 **完整社区 [sub2api](https://github.com/Wei-Shaw/sub2api)** 之上，叠加「闲置 TOKEN」C 端能力。

## 产品形态

| 面 | 路径 | 说明 |
|---|---|---|
| **管理端** | 社区 `frontend/` 的 `/admin` | **sub2api 原管理端**；闲猫能力在侧栏「闲猫」增量组 |
| **C 端** | `market/web` 的 `/` | 闲置 TOKEN：市场 / 买家 / 卖家 |
| 市场后端 | `market/` | Edge、挂单、质检、账本等增量 |

详见本仓：`docs/adr/0001-*.md`、`docs/dev/admin-c-end-split.md`。

## 本地测试（单端口）

```bash
cd market && scripts/dev.sh reset && scripts/dev.sh up
```

打开 `http://127.0.0.1:8080`：

- C 端：`/`
- 管理端：首页「控制台」进入 `/admin/dashboard`（sub2api 底座）。侧栏「闲猫」是增量；闲猫接口直接用当前管理员会话，不再二次登录。

完整点检见 [`market/TEST-LOCAL.md`](market/TEST-LOCAL.md)。
