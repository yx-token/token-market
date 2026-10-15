# 店铺活动商品后端进度

已完成商品 CRUD 和 HTTP handler；路由由主代理在共享 `server.go` 接入。

## 最终调用接口

`internal/listing/credit_offers.go` 导出：

- `CreateCreditOffer(ctx, db DB, sellerID int64, in CreditOfferInput) (*CreditOffer, error)`
- `UpdateCreditOffer(ctx, db DB, sellerID, id int64, in CreditOfferInput) (*CreditOffer, error)`
- `ListCreditOffers(ctx, db DB, sellerID int64, activeOnly bool) ([]CreditOffer, error)`
- `GetCreditOffer(ctx, db DB, id int64) (*CreditOffer, error)`

商品字段和 JSON 字段严格复用 billing-contract：`price_cny` 是人民币售价，`face_value_usd` 是美元面额。商品层不换算币种；买包时由资金服务快照 `credit_usd_to_balance_rate`。不接触旧 quota_packs、已购活动包快照和资金余额。

HTTP handler 已实现：

- `POST /api/seller/credit-offers` → `createCreditOffer`，201。
- `PUT /api/seller/credit-offers/{id}` → `updateCreditOffer`，200。
- `GET /api/seller/credit-offers` → `sellerCreditOffers`，200，含暂停商品。
- `GET /api/market/credit-offers?seller_id=123` → `marketCreditOffers`，200，仅本店 active 商品；无有效 seller_id 为400。

卖家接口须放在现有认证路由中。seller_id 只能从登录身份取，请求体带 seller_id 会被未知字段校验拒绝。更新不存在/非本人商品统一404。列表空值为 `[]`，作用域空值为 `[]`。

## 输入与作用域

- 售价与面额均为有限正数，最多2位小数，最大1,000,000,000；在 SQL numeric 写入前拒绝多余小数，避免静默四舍五入。
- 标题 trim 后1至120个字符；描述 trim 后最多4000字符。
- status 仅 active / paused，创建默认 active；更新省略 status 保持原状态，避免编辑停售商品自动上架。
- allowed_platforms trim、去重，必须为 vendor_configs 中当前 enabled 的真实厂商，最多64项。
- allowed_models trim、去重，保留大小写与精确模型名，拒绝 * / ? 通配符，最多256项。
- 空数组表示无限制；两个范围同时填写时应在消费侧同时匹配，且始终限定商品 seller_id。

## 验证

- `go test ./internal/listing` 通过（含金额 NaN/Inf、超额、小数位、范围规范化与通配拒绝）。
- `TM_TEST_DATABASE_DSN='postgres://tm:tm@127.0.0.1:55432/tm?sslmode=disable' go test -tags=integration ./internal/listing -run TestCreditOffersSellerIsolationAndScopes -count=1 -v` 通过。
- 集成验证覆盖：真实迁移、范围厂商有效性、跨卖家更新404、公开列表隐藏暂停/其他卖家商品、编辑暂停状态保留、已购包面额/价格/范围快照不受编辑影响。
- `go test ./internal/listing ./internal/httpapi` 通过；HTTP 测试覆盖无效/缺失 seller_id 400、seller_id 请求体注入被拒绝。
- `go test -race ./internal/listing -run 'TestCreditOffer|TestNormalizeCreditOffer' -count=1` 通过。

未改共享 server.go、listing/model.go/listings.go、前端文件；未 commit 或部署。
