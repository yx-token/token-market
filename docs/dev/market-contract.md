# 商家清单与未检测挂单约定

需求 1、4，由 market 工人负责。不要改动活动额度包买家功能。

## 商家清单

- `GET /api/buyer/merchants`：当前买家持久化商家清单，返回 `[{seller_id, added_at, listings: MarketItem[]}]`。商家身份为 seller_id；listings 仅包含当前买家可见且可用挂单，无可用挂单仍保留商家。
- `PUT /api/buyer/merchants/{seller_id}`：幂等加入商家；该商家必须至少有一个当前可用市场挂单。
- `DELETE /api/buyer/merchants/{seller_id}`：幂等移除并立即重发相关 Key 路由，移除商家的跳不再调用。其他商家和平台兜底保持顺序。
- `GET /api/buyer/merchants/listings`：清单内全部可用挂单（MarketItem[]），供 API Key 页选择。
- 市场 `MarketItem` 新增 `seller_id: number`。市场按钮先加入商家清单，再去 Key 页面从清单内选挂单、调整顺序。Key 创建/编辑服务端拒绝清单外的 listing_id，发布路由也过滤已移除商家及停用、隔离挂单。
- 迁移 `0036_buyer_merchants.sql` 从未删除的存量 Key 挂单、升级前尚未履约的 `pending pack_checkouts` 回填 `(user_id, seller_id)`，避免旧 Key 断路或旧订单无法交付。平台默认分组不受商家清单限制。

## 未检测挂单（卖家 UI 对齐）

- 完成有效授权后仍需明确选择非空模型白名单。`PUT /api/seller/upstreams/{id}/models` 保存并同步白名单成功后，正常卖家账号直接进入 `ready`，不自动跑质量检测；隐私保护检查继续强制执行。基线账号保持原检测流程。
- 卖家 `ready` 后可直接 `POST .../listings` 寄售/额度包挂单；未检测不能按检测通过或估价通过展示。平台回收仍要求有效检测证据。
- `POST /api/seller/upstreams/{id}/retry-verification` 允许 `ready` 或 `verify_failed` 主动发起检测；不允许借跳过检测恢复 suspended/removing/removed、隐私失败或无授权账号。已检测不健康、隔离账号继续受现有边界约束。
- 市场、模型列表和商家清单新增 `detection_status: 'untested'|'tested'`，并沿用 `last_checked_at`。无有效检测证据一律显示“未检测”；不得用默认 grade/Q/health 推断通过。
- 市场模型新增 `model_checks`，每个声明的模型返回 `{model, detection_status, degradation_status?}`。仅 `gpt-6-astra` 展示降智检测结果，`degradation_status` 为 `untested|passed|failed|inconclusive`；无该模型有效证据必须是 untested，其他模型不展示降智结果。
- 模型行和总体状态共用有效证据过滤：同上游已完成运行、冻结检测设置、逐项定义一致、有效期内且未被重置的实际观测。无 run_id、失败运行或过期定义不能显示模型通过。未检测账号仅进入显式挂单分组，不进入平台默认保障池；授权恢复也不能改变该边界。
- 不输出内部探测参数、凭证、组 ID 或敏感质检证据。

## 活动额度包入口

市场详情加入 `RouterLink :to="{path:'/buyer/store-credits',query:{seller_id:item.seller_id}}"`，文案“店铺活动额度包”。主代理负责页面、API 模块和可选 `StoreCreditOffers.vue` 组件，market 工人不实现活动包购买。

## 协作范围

market 工人小范围修改 listing/onboarding.go、listings.go、model_catalog.go，不覆盖账务工人活动包改动。独立 API 模块和迁移，不整体格式化共享文件，不提交、不部署。最终浏览器验收由主代理执行。
