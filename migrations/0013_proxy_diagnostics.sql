-- 保留最近一次连通性与质量检测，刷新页面后仍可查看报告。
ALTER TABLE proxies ADD COLUMN connection_report jsonb;
ALTER TABLE proxies ADD COLUMN quality_report jsonb;
ALTER TABLE proxies ADD COLUMN connection_checked_at timestamptz;
ALTER TABLE proxies ADD COLUMN quality_checked_at timestamptz;
