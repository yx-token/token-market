# 本地可测清单（单端口）

## 启动

```bash
cd market
scripts/dev.sh reset   # 首次或数据脏了
scripts/dev.sh up
```

浏览器只开：`http://127.0.0.1:8080`

## 账号

| 角色 | 邮箱 | 密码 |
|---|---|---|
| 管理员 | `admin@dev.local` | `admin-password-1` |
| 卖家 | `seller@dev.local` | `seller-password-1` |
| 买家 | `buyer@dev.local` | `buyer-password-1` |

## 入口

| 面 | 地址 |
|---|---|
| C 端市场 | `/` |
| **管理端**（sub2api 壳 + 侧栏「闲猫市场」） | `/admin` |
| 首页「控制台」 | 已登录则换发会话后进入 `/admin/dashboard` |
| `/console`、旧 `/engine` | 跳到 `/admin/dashboard` |

## 建议点检

1. 首页是闲猫 C 端，不是管理壳
2. 点「控制台」进入管理端（侧栏品牌「闲猫」；概览、用户、分组、账号、代理）
3. 侧栏「闲猫市场」是增量：供给、质检、资金、分类、支付（不再二次登录）
4. 卖家 / 买家登录 C 端流程

自动化全流程：

```bash
scripts/flow-e2e.sh
```
