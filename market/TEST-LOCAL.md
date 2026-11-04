# 本地可测清单（单端口）

真实引擎模式是默认模式：`market/scripts/dev.sh` 启动 sub2api，并将闲猫嵌入宿主。
本地支付宝 / 微信支付请求由 `fake-thirdparty` 接收，不会产生真实扣款。
`TM_PAYMENT_ALIPAY_GATEWAY`、`TM_PAYMENT_WECHAT_BASE_URL` 仅由脚本在宿主进程设置，生产环境不要设置。

## 启动

```bash
cd market
scripts/dev.sh reset   # 首次或数据脏了
scripts/dev.sh up
```

浏览器只开：`http://127.0.0.1:8080`

如果默认端口被占用，可通过 `DEV_PG_PORT`、`DEV_REDIS_PORT`、`DEV_ENGINE_PORT` 和对应 `DEV_*_CONTAINER` 环境变量隔离。
站点、Edge 与第三方替身端口分别可通过 `DEV_SITE_PORT`、`DEV_EDGE_PORT`、`DEV_THIRDPARTY_PORT` 覆盖，默认仍为 8080、8090、18082；多套环境同时运行时也要指定独立的 `DEV_DATA_DIR`。

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
3. 侧栏「闲猫市场」是增量：供给、质检、资金、分类（不再二次登录）；支付配置在 sub2api 的「设置 → 支付设置」
4. 卖家 / 买家登录 C 端流程

## 支付点检

真实引擎模式会在 sub2api 中创建或更新名为 `dev-fake-alipay`、`dev-fake-wxpay` 的官方服务商实例，并启用 sub2api 支付总开关。该配置使用 `data/dev/pay-keys/` 下生成的开发密钥，回调 origin 指向本机 `http://127.0.0.1:8080`。

1. 进入 sub2api「设置 → 支付设置」，确认全局支付已启用，支付宝官方和微信官方实例处于启用状态。
2. 使用买家账号打开钱包充值，确认可用渠道来自上述官方实例；新订单不再提供人工转账。
3. 创建支付宝或微信充值单，在 fake-thirdparty 收银台完成模拟付款，确认钱包余额到账，充值订单状态更新。
4. 如需排查失败，可看 `data/dev/fake-thirdparty.log` 与 `data/dev/sub2api-data/server.log`；成功与否以充值订单状态和钱包余额为准。查单、关单和原路退款也应继续指向下单时记录的实例。

`USE_REAL_ENGINE=0` 的替身引擎没有注册 hostpay，因此不会配置在线支付，钱包不显示在线渠道。历史人工充值单的管理端确认到账流程仍保留。

自动化全流程：

```bash
scripts/flow-e2e.sh
```
