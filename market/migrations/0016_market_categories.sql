-- 市场分类树：控制台维护；卖家挂单勾选 seller_pickable=true 的叶子（或节点）
CREATE TABLE market_categories (
    id               text PRIMARY KEY,
    parent_id        text REFERENCES market_categories (id) ON DELETE RESTRICT,
    name             text        NOT NULL,
    description      text        NOT NULL DEFAULT '',
    sort             int         NOT NULL DEFAULT 0,
    enabled          boolean     NOT NULL DEFAULT true,
    seller_pickable  boolean     NOT NULL DEFAULT false,
    icon             text        NOT NULL DEFAULT '',
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    CHECK (id <> ''),
    CHECK (parent_id IS NULL OR parent_id <> id)
);
CREATE INDEX market_categories_parent_idx ON market_categories (parent_id, sort, id);
CREATE INDEX market_categories_seller_idx ON market_categories (seller_pickable) WHERE enabled AND seller_pickable;

COMMENT ON TABLE market_categories IS '市场分类树；listings.categories 存叶子/可选节点 id';
COMMENT ON COLUMN market_categories.seller_pickable IS '卖家发布挂单时可勾选';

-- 常用初始化（幂等）
INSERT INTO market_categories (id, parent_id, name, description, sort, seller_pickable, icon) VALUES
  ('vendor',   NULL, '厂商专场', '按上游厂商浏览', 10, false, 'chat'),
  ('offer',    NULL, '供给形态', '按量 / 额度包 / 保障池', 20, false, 'tag'),
  ('scene',    NULL, '场景标签', '卖家可勾选的专场标签', 30, false, 'code'),
  ('region',   NULL, '地区站点', '国际站 / 国内站', 40, false, 'more'),

  ('claude',   'vendor', 'Claude',   'Anthropic Claude', 10, false, ''),
  ('chatgpt',  'vendor', 'ChatGPT',  'OpenAI ChatGPT / Codex', 20, false, ''),
  ('gemini',   'vendor', 'Gemini',   'Google Gemini', 30, false, ''),
  ('kimi',     'vendor', 'Kimi',     '月之暗面 Kimi Coding', 40, false, ''),
  ('zhipu',    'vendor', '智谱',     '智谱 GLM Coding', 50, false, ''),
  ('minimax',  'vendor', 'MiniMax',  'MiniMax Coding', 60, false, ''),
  ('grok',     'vendor', 'Grok',     'xAI Grok', 70, false, ''),
  ('other_vendor', 'vendor', '其他厂商', 'Antigravity / OpenCode 等', 80, false, ''),

  ('channel',  'offer', '按量通道', '寄售按量计费', 10, false, ''),
  ('quota_pack','offer', '额度包',  '整包预付、可退未用量', 20, false, ''),
  ('pool',     'offer', '保障池',   '平台保障池调度', 30, false, ''),

  ('low_rate', 'scene', '低倍率',   '倍率偏低的通道', 10, true, ''),
  ('coding',   'scene', '写代码',   'Coding Plan / 写代码场景', 20, true, ''),
  ('featured', 'scene', '精选推荐', '可被运营位收录', 30, true, ''),
  ('grade_s',  'scene', 'S 级优选', '质量等级 S', 40, true, ''),
  ('refundable','scene','可退货',   '额度包未用量可退', 50, true, ''),
  ('long_ctx', 'scene', '长上下文', '适合长文档 / 大仓', 60, true, ''),
  ('stable',   'scene', '高稳定',   '成功率与延迟更稳', 70, true, ''),
  ('cheap',    'scene', '特惠',     '价格敏感买家', 80, true, ''),

  ('intl',     'region', '国际站',  '海外官方站点', 10, true, ''),
  ('cn',       'region', '国内站',  '国内官方站点', 20, true, '')
ON CONFLICT (id) DO NOTHING;
