-- 市场热搜初始化：六个常用快捷筛选（黄条全部展示，货架 pill 取前四项）
-- Claude Max / ChatGPT Plus / Kimi Coding / 低倍率 / 保障池 / 订阅套餐

INSERT INTO settings (key, value, updated_at) VALUES (
  'market_discovery',
  '{
    "version": 2,
    "hot_bar": {"enabled": true},
    "feed": {"pin_pool_first": true},
    "hot_searches": [
      {"id": "hs_gpt", "label": "ChatGPT Plus", "enabled": true, "sort": 10, "action": {"q": "ChatGPT"}},
      {"id": "hs_claude", "label": "Claude Max", "enabled": true, "sort": 20, "action": {"q": "Claude"}},
      {"id": "hs_kimi", "label": "Kimi Coding", "enabled": true, "sort": 30, "action": {"q": "Kimi"}},
      {"id": "hs_low", "label": "低倍率", "enabled": true, "sort": 40, "action": {"tag": "low_rate"}},
      {"id": "hs_pool", "label": "保障池", "enabled": true, "sort": 50, "action": {"plat": "保障池"}},
      {"id": "hs_pack", "label": "订阅套餐", "enabled": true, "sort": 60, "action": {"kindTab": "pack"}}
    ]
  }'::jsonb,
  now()
)
ON CONFLICT (key) DO UPDATE SET
  value = (settings.value - 'category_groups' - 'slots')
    || jsonb_build_object(
      'version', 2,
      'hot_searches', '[
        {"id": "hs_gpt", "label": "ChatGPT Plus", "enabled": true, "sort": 10, "action": {"q": "ChatGPT"}},
        {"id": "hs_claude", "label": "Claude Max", "enabled": true, "sort": 20, "action": {"q": "Claude"}},
        {"id": "hs_kimi", "label": "Kimi Coding", "enabled": true, "sort": 30, "action": {"q": "Kimi"}},
        {"id": "hs_low", "label": "低倍率", "enabled": true, "sort": 40, "action": {"tag": "low_rate"}},
        {"id": "hs_pool", "label": "保障池", "enabled": true, "sort": 50, "action": {"plat": "保障池"}},
        {"id": "hs_pack", "label": "订阅套餐", "enabled": true, "sort": 60, "action": {"kindTab": "pack"}}
      ]'::jsonb,
      'hot_bar', COALESCE(settings.value->'hot_bar', '{"enabled": true}'::jsonb),
      'feed', COALESCE(settings.value->'feed', '{"pin_pool_first": true}'::jsonb)
    ),
  updated_at = now();
