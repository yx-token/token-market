-- 0011 补齐 sub2api 支持的订阅接入方式：
--   Gemini Google One（个人订阅 Google AI Pro / Ultra，原有 oauth 即 GCP Code Assist）；
--   智谱、MiniMax Coding Plan 国际站（z.ai、minimax.io，原有 coding 即国内站）。
-- 管理员可在厂商配置中选择开放哪些接入方式。

UPDATE vendor_configs SET auth_types = '{google_one,oauth}', updated_at = now()
WHERE platform = 'gemini' AND auth_types = '{oauth}';

UPDATE vendor_configs SET auth_types = '{coding,coding-intl}', updated_at = now()
WHERE platform IN ('zhipu', 'minimax') AND auth_types = '{coding}';
