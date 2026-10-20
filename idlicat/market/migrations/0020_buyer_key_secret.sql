-- 0020 买家 Key 明文用密钥环加密保存（AAD = buyer-key:<id>），买家可以随时在控制台再次查看、复制。
-- 本迁移之前创建的 Key 只存了哈希，为空，无法再次查看。

ALTER TABLE buyer_keys ADD COLUMN key_enc bytea;
