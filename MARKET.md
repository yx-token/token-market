# 闲猫产品说明

本仓库即闲猫（IdleCat）产品代码：网关、管理端与闲置 TOKEN 市场同仓同端口。

## 产品形态

| 面 | 路径 | 说明 |
|---|---|---|
| **管理端** | `frontend/` → `/admin` | 运营控制台；侧栏「闲猫」为市场供给 / 资金 / 质检 / 运营 |
| **C 端** | `market/web` → `/` | 市场 / 买家 / 卖家 |
| **市场后端** | `market/` | Edge、挂单、质检、账本等 |

并仓决策与模块地图：`docs/adr/0001-*.md`、`docs/dev/admin-c-end-split.md`。

## 本地测试（单端口）

```bash
cd market && scripts/dev.sh reset && scripts/dev.sh up
```

打开 http://127.0.0.1:8080 ：

- C 端：`/`
- 管理端：首页「控制台」进入 `/admin/dashboard`；闲猫接口使用当前管理员会话，无需二次登录

完整点检见 [`market/TEST-LOCAL.md`](market/TEST-LOCAL.md)。
