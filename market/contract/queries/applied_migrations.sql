-- 兼容自检：sub2api 已执行的迁移
SELECT filename, checksum
FROM schema_migrations
ORDER BY filename
