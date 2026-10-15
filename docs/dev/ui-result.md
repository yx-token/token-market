# 卖家中心 UI 结果

状态：界面已落地，等待主代理用现有 ego TaskSpace 118 验收。未新开浏览器，未改买家用量/账单分摊，未改后台概览。

主代理后续验收补记：八个卖家页面均已实际在桌面与390px手机检查。发现顶栏横向溢出后由原 UI 工人修复 `xianmao-market.css`，重新构建后八页 `clientWidth=scrollWidth=390`；320px 顶栏、搜索和账号菜单也可使用。前端239项测试、类型检查及最终生产构建通过。完整购买、调用、结算、退款和后台设置验证见 `market-upgrade-acceptance.md`。

## 已改页面

- `web/src/views/seller/EarningsView.vue`：冻结小时说明、提现表单内联校验、载入失败可重试。
- `web/src/views/seller/UsageView.vue`：入账即结算、冻结后才可提现的说明，用量载入失败可重试。
- `web/src/views/seller/ListingsView.vue`：状态/形态筛选；未检测挂单标「未检测」，不把缺省等级当成通过。
- `web/src/views/seller/UpstreamsView.vue`：进度筛选；待挂单可直接发布，也可主动发起质量检测。
- `web/src/views/seller/UpstreamView.vue`：未检测不展示通过结论；保障池和平台回收禁用；隐私失败不能挂单；`ready` 可 `retry-verification`。
- `web/src/views/seller/PublishView.vue`：保存模型后可直接挂单；「确认模型并开始检测」仍走可选检测。未检测不能进保障池或回收。
- `web/src/views/seller/ProxiesView.vue`：区分代理连通性检测和账号质量检测。
- `web/src/views/seller/StoreCreditsView.vue`：本店活动额度上架/编辑/停售。实付人民币、美元额度、厂商/模型范围、用尽后转平台余额都写在表单预览里。
- `web/src/views/admin/SettingsView.vue`：读写 `freeze_hours`（默认 24）和 `credit_usd_to_balance_rate`。保存时不回写 `freeze_days`，避免小时被天数进位覆盖。

样式在 `web/src/styles/seller-center.css`，卖家导航增加 `/seller/credits`。

## 接口对齐

- 冻结：`docs/dev/settlement-contract.md` 的 `freeze_hours`。公开配置缺字段时才用 `freeze_days * 24`。
- 活动包：`docs/dev/billing-contract.md`。卖家 API 在 `web/src/api/sellerStoreCredits.ts`，类型复用 `storeCreditsBuyer.ts` 的 `CreditOffer`。不实现买家购买页。
- 未检测挂单：`docs/dev/market-contract.md`。没有新的 skip 接口。`PUT models` 后 `ready` 可直接挂寄售/额度包；`retry-verification` 用于主动检测。只有 `detection_status === 'tested'` 视为已检测。`gpt-6-astra` 降智行复用 `MarketDetectionSummary`。

## 空列表

`GET /api/seller/listings` 成功时可以返回 `null`。卖家页不再把 `null` 当成载入失败：列表请求把 `null` 收成 `[]`，失败才写独立 error。上游、收益明细、提现记录、用量上游列表同样处理。代理页原有 `?? []`，没有新增误判。回归：`seller-empty.spec.ts`。

活动预览改为：按当前兑换率，$1 额度可抵 ¥X 店铺消费；购买后按购买时兑换率使用，后续调整不影响已购包。

## 验证

- `pnpm exec vue-tsc --noEmit` 通过。
- `seller-center.spec.ts`、`publish-models.spec.ts`、`seller-retest.spec.ts` 共 14 项通过。
- 已去掉 `EarningsView.vue` 未使用的 `toast`。

## 未覆盖

- 未改 `OnboardingWizard.vue`，该通用接入组件由另一工人处理。
- 未改买家活动包页、买家用量/账单、后台概览。
