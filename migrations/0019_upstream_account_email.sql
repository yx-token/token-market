-- 0019 上游的官方账号邮箱：授权或导入时从令牌信息中识别（与 sub2api 账号列表的取值一致），
-- 作为卖家和管理员看到的账号名称；Gemini 与 Coding Plan 拿不到邮箱，仍按上游编号展示。
-- 本迁移之前接入的上游由调度器从 sub2api 账号 extra 中的邮箱补齐。

ALTER TABLE upstreams ADD COLUMN account_email text;
