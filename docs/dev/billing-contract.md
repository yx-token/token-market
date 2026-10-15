# 店铺活动额度包与冻结结算契约

状态：后端已实现，专项测试已通过，等待主代理统一验收。独立活动包不改变旧 quota_packs / 旧包专用 Key。

## 币种边界（重要）

历史 D16（docs/08-billing.md:25）：ledger 买家现金、人民币充值/提现、sub2api 用户余额 1 单位 = ¥1；usage.total_cost 是官方美元数值，usage.actual_cost 已乘店铺/分组实际计费倍率，数值直接作为上述平台余额单位扣减。**本次不全局改变钱包或既有按量费率。**

新增平台设置 `credit_usd_to_balance_rate`：1 美元活动额度兑换的平台余额单位数，默认 1，必须 >0。与人民币余额口径对应，含义是活动包 USD→CNY 记账换算率。后台 GET/PUT `/api/admin/settings` 复用现有设置接口。购买时快照为 `usd_to_balance_rate`，后续后台改价不追改已购包。

上游 `users.balance` 实际为 `numeric(20,8)`，而 usage 与本地资金余量保留10位。为避免面额变成零或隐性改汇率，购买要求 `face_value_usd * usd_to_balance_rate` 在 `1e-8..1e9` 且可精确表示为8位小数，否则返回400 `INVALID_CREDIT_CONVERSION`，不扣款。充值直接发送数据库换算结果，不在 Go 重算乘积；活动钱包创建显式初始余额0，不继承 sub2api 新用户默认赠金。

例：售价 ¥30、面额 $10、兑换率 7，则可抵本店 70 平台余额单位。一次调用 sub2api actual_cost=3.5（已经含店铺倍率）消耗 $0.5，包内剩 $9.5；实际已收预付款释放 ¥1.5。包余额只剩 $0.1 时，本次 actual_cost=3.5 分摊包抵扣 0.7 单位（$0.1）和平台现金 2.8 元。不会再给调用金额重复乘店铺倍率，也不会全局把现金扣费乘汇率。

卖家优惠自己承担：活动覆盖部分的卖家收益按实收售价/可抵平台额度的比例缩放；平台抽佣相同比例缩放。每笔保留抵扣美元、抵扣平台单位、释放人民币预付、平台余额补差与对应 usage 记录。

## HTTP API

沿用现有 `{data: ...}` 成功信封及登录身份。id 为数字，金额为 JSON number，人民币售价与美元面额最多2位小数。

- `POST /api/seller/credit-offers` 创建：`{title, description?, price_cny, face_value_usd, allowed_platforms?: string[], allowed_models?: string[]}`。seller_id 从登录身份获取，禁止代别的店建活动。空数组表示本店所有厂商/模型；限制同时存在时须同时匹配。price_cny 与 face_value_usd >0。
- `GET /api/seller/credit-offers` 返回 `CreditOffer[]`（含停售活动）。
- `PUT /api/seller/credit-offers/{id}` 更新：同创建字段，可附 `status: "active"|"paused"`；全量编辑商品信息，已购包快照不变。
- `GET /api/market/credit-offers?seller_id=123` 返回该店 active 活动 `CreditOffer[]`；seller_id 必填。
- `POST /api/buyer/credit-packs` 余额购买：`{offer_id, idempotency_key}`，返回 `CreditPack`。幂等键买家范围唯一，重复请求返回原包，不重复扣款；同键不同 offer 返回409。现阶段使用平台余额支付，用户可通过既有充值接口先充值。
- `GET /api/buyer/credit-packs` 返回 `CreditPack[]`。
- `POST /api/buyer/credit-packs/{id}/refund` 退未使用部分到平台余额；按未用面额对应实际人民币预付退款，不能把赠送额度兑现。已开始使用/正在请求的包需待用量完成后结算，具体可退状态在最终记录补充。

`CreditOffer`：`{id, seller_id, title, description, price_cny, face_value_usd, allowed_platforms:[], allowed_models:[], status, created_at, updated_at}`。

`CreditPack`：`{id, offer_id, buyer_id, seller_id, title, price_cny, face_value_usd, remaining_usd, remaining_balance_units, prepaid_remaining_cny, usd_to_balance_rate, allowed_platforms:[], allowed_models:[], status, created_at}`。状态 `provisioning|active|depleted|refunding|refunded`；购买可先返回 provisioning，后台出站扣款/开通后 active。包不签发前台独立买家 Key，普通买家 Key 路由到本店且匹配厂商/模型时优先消费；本店多上游共享优惠，绝不用于其他店。耗尽后自动沿原 Key 使用平台余额。当笔不足的差额亦转平台余额。

## 资金与并发约束

优惠面额不充入买家的全平台影子余额。活动包拥有隔离的 sub2api 影子钱包（只允许本店挂单分组，凭证不暴露给前端）；Edge 按登录买家 Key 路由和实际模型挑匹配包，再回到该 Key 原平台余额路径。预付款记独立托管，使用才按比例释放，卖家走现有冻结收益。

购买、用量分摊、退款均采用数据库事务、行锁/唯一幂等键与既有 sub2api_ops outbox。用量按 sub2api_usage_id 唯一，postUsage 并发认领不能重复结算；分摊不得超过 remaining，最后一笔释放剩余精确预付避免舍入残留。原包、退款与对账口径保持兼容。

## 冻结设置

平台 `freeze_hours` 默认24；兼容旧 `freeze_days` 输入。worker每分钟推进，按小时桶结束后满冻结期才释放（不会因 date_trunc 提前）。详细进度见 settlement-contract.md。

## 已接通的报表字段

`GET /api/buyer/usage` 与请求明细逐跳项新增 `cash_cost`（本次扣平台现金，元）、`credit_prepaid_cny`（消耗活动预付款，元）、`credit_covered_usd`（抵扣活动美元）、`credit_covered_units`（抵扣平台单位）、`credit_pack_id?`。`cost = COALESCE(cash_cost,actual_cost)+credit_prepaid_cny` 为人民币实际消费；普通流量与旧包相应活动字段为0。`GET /api/buyer/bills` 按日累计前四个金额字段，不返回单一 pack_id。

平台 `buyer_balances` 包含普通现金与活动预付两类负债，新增 `buyer_store_credit` 单列活动预付（不是另一笔可与buyer_balances重复相加的金额）。独立活动影子钱包单独对账，不混入普通现金/余额退款。

退款会先返回 `refunding`，等待至少原用量安全延迟+1分钟，且实际活动钱包余额、已入账余量与在途资金全部一致后，先撤销隔离额度，再把**剩余实付人民币**退回普通余额；出站操作完成后 `refunded`。余额不足导致最后一笔现金补差无法立即扣款时会保留欠款账本/outbox；买家尚有未完成补差期间停止活动准入，充值后资金worker自动重试原幂等扣款。

退款余额核对允许已入账消费及已完成充值/补回各自能解释的8位舍入误差之和（另加1e-9表示误差），不使用任意大额容差；撤销金额使用实际远端8位余额，返还现金仍按本地剩余实付人民币，赠送额度不能兑现。不明余额差异和未完成出站继续阻止退款。

实现保留用量2分钟安全延迟（原有防晚提交机制），每轮及时入账/分摊；解冻默认24小时，小时桶保守延后小于1小时并每分钟推进。修正了旧增量扫描 overlap==batch 导致4000条以上积压无法前进的问题，读取上限现为 overlap+batch。
