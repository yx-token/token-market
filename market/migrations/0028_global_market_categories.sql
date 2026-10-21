-- 全球通用市场分类扩充（幂等）
-- 参考：OpenRouter use-case categories、KYMA type/use_case、主流 API 市场 modality/capability 习惯
-- 保留 0016 已有节点；只 INSERT 新 id，不覆盖运营已改过的名称

-- ========== 一级：用途（全球行业 / 场景，卖家可勾选）==========
INSERT INTO market_categories (id, parent_id, name, description, sort, seller_pickable, icon) VALUES
  ('usecase', NULL, '用途场景', 'Global use cases aligned with OpenRouter / API marketplaces', 50, false, 'code')
ON CONFLICT (id) DO NOTHING;

INSERT INTO market_categories (id, parent_id, name, description, sort, seller_pickable, icon) VALUES
  ('uc_programming',  'usecase', '编程开发',     'Programming / write-code（OpenRouter: programming）', 10, true, ''),
  ('uc_agents',       'usecase', '智能体 / 工具', 'Build agents & tool-use workflows', 20, true, ''),
  ('uc_chat',         'usecase', '通用对话',     'General chat / assistant', 30, true, ''),
  ('uc_roleplay',     'usecase', '角色扮演',     'Roleplay / character（OpenRouter: roleplay）', 40, true, ''),
  ('uc_content',      'usecase', '内容写作',     'Write content / copywriting', 50, true, ''),
  ('uc_marketing',    'usecase', '营销增长',     'Marketing（OpenRouter: marketing）', 60, true, ''),
  ('uc_seo',          'usecase', 'SEO / 获客',   'Marketing SEO（OpenRouter: marketing/seo）', 70, true, ''),
  ('uc_translation',  'usecase', '翻译本地化',   'Translation & localization（OpenRouter: translation）', 80, true, ''),
  ('uc_research',     'usecase', '调研检索',     'Research / web research / RAG', 90, true, ''),
  ('uc_documents',    'usecase', '文档分析',     'Analyse documents / OCR / summarization', 100, true, ''),
  ('uc_math',         'usecase', '数学推理',     'Solve math / quantitative reasoning', 110, true, ''),
  ('uc_science',      'usecase', '科学理工',     'Science & STEM（OpenRouter: science）', 120, true, ''),
  ('uc_technology',   'usecase', '技术运维',     'Technology / DevOps / IT（OpenRouter: technology）', 130, true, ''),
  ('uc_legal',        'usecase', '法律合规',     'Legal（OpenRouter: legal）', 140, true, ''),
  ('uc_finance',      'usecase', '金融财务',     'Finance（OpenRouter: finance）', 150, true, ''),
  ('uc_health',       'usecase', '健康医疗',     'Health（OpenRouter: health）— 非诊疗建议', 160, true, ''),
  ('uc_academia',     'usecase', '学术教育',     'Academia / education（OpenRouter: academia）', 170, true, ''),
  ('uc_trivia',       'usecase', '百科问答',     'Trivia / general knowledge（OpenRouter: trivia）', 180, true, ''),
  ('uc_customer',     'usecase', '客户支持',     'Customer support / helpdesk', 190, true, ''),
  ('uc_data',         'usecase', '数据分析',     'Data analysis / spreadsheet / BI', 200, true, '')
ON CONFLICT (id) DO NOTHING;

-- ========== 一级：能力（全球 modality / capability，卖家可勾选）==========
INSERT INTO market_categories (id, parent_id, name, description, sort, seller_pickable, icon) VALUES
  ('capability', NULL, '能力标签', 'Global capabilities & modalities (vision / tools / reasoning / media)', 60, false, 'tag')
ON CONFLICT (id) DO NOTHING;

INSERT INTO market_categories (id, parent_id, name, description, sort, seller_pickable, icon) VALUES
  ('cap_vision',      'capability', '视觉理解',     'Vision / image input', 10, true, ''),
  ('cap_tools',       'capability', '函数调用',     'Tool use / function calling', 20, true, ''),
  ('cap_reasoning',   'capability', '深度推理',     'Reasoning / chain-of-thought models', 30, true, ''),
  ('cap_structured',  'capability', '结构化输出',   'Structured outputs / JSON schema', 40, true, ''),
  ('cap_caching',     'capability', '提示缓存',     'Prompt caching (implicit / explicit)', 50, true, ''),
  ('cap_file',        'capability', '文件输入',     'File / PDF / long document input', 60, true, ''),
  ('cap_web',         'capability', '联网搜索',     'Web search / browsing', 70, true, ''),
  ('cap_image_gen',   'capability', '图像生成',     'Image generation / editing', 80, true, ''),
  ('cap_video',       'capability', '视频',         'Video understanding or generation', 90, true, ''),
  ('cap_audio',       'capability', '语音音频',     'Speech / transcription / TTS / audio', 100, true, ''),
  ('cap_embedding',   'capability', '向量嵌入',     'Embeddings / retrieval vectors', 110, true, ''),
  ('cap_rerank',      'capability', '重排序',       'Reranking models', 120, true, ''),
  ('cap_realtime',    'capability', '实时语音',     'Realtime / websocket voice', 130, true, ''),
  ('cap_fast',        'capability', '低延迟',       'Fast / low-latency tier', 140, true, ''),
  ('cap_batch',       'capability', '批量推理',     'Batch / offline high-throughput', 150, true, '')
ON CONFLICT (id) DO NOTHING;

-- ========== 扩充厂商（全球常见，浏览用，不可勾选）==========
INSERT INTO market_categories (id, parent_id, name, description, sort, seller_pickable, icon) VALUES
  ('deepseek',    'vendor', 'DeepSeek',    'DeepSeek (CN / global)', 90, false, ''),
  ('qwen',        'vendor', '通义千问',    'Alibaba Qwen / DashScope', 100, false, ''),
  ('mistral',     'vendor', 'Mistral',     'Mistral AI (EU)', 110, false, ''),
  ('meta_llama',  'vendor', 'Llama / Meta','Meta Llama open-weight family', 120, false, ''),
  ('cohere',      'vendor', 'Cohere',      'Cohere (CA / global)', 130, false, ''),
  ('perplexity',  'vendor', 'Perplexity',  'Perplexity AI', 140, false, ''),
  ('amazon',      'vendor', 'Amazon',      'Amazon Bedrock / Nova', 150, false, ''),
  ('microsoft',   'vendor', 'Microsoft',   'Azure OpenAI / Copilot family', 160, false, ''),
  ('nvidia',      'vendor', 'NVIDIA',      'NVIDIA NIM / foundation models', 170, false, ''),
  ('baidu',       'vendor', '百度',        'Baidu ERNIE', 180, false, ''),
  ('bytedance',   'vendor', '字节跳动',    'ByteDance Doubao / Seed', 190, false, ''),
  ('tencent',     'vendor', '腾讯',        'Tencent Hunyuan', 200, false, ''),
  ('ai21',        'vendor', 'AI21',        'AI21 Labs Jamba', 210, false, ''),
  ('inflection',  'vendor', 'Inflection',  'Inflection Pi', 220, false, '')
ON CONFLICT (id) DO NOTHING;

-- ========== 扩充地区（全球站点 / 合规区域，卖家可勾选）==========
INSERT INTO market_categories (id, parent_id, name, description, sort, seller_pickable, icon) VALUES
  ('us',     'region', '美国站',   'United States endpoint / account', 30, true, ''),
  ('eu',     'region', '欧洲站',   'EU / GDPR-oriented endpoint', 40, true, ''),
  ('uk',     'region', '英国站',   'United Kingdom', 50, true, ''),
  ('jp',     'region', '日本站',   'Japan', 60, true, ''),
  ('kr',     'region', '韩国站',   'South Korea', 70, true, ''),
  ('sg',     'region', '新加坡站', 'Singapore / SEA hub', 80, true, ''),
  ('in',     'region', '印度站',   'India', 90, true, ''),
  ('br',     'region', '巴西站',   'Brazil / LATAM hub', 100, true, ''),
  ('au',     'region', '澳洲站',   'Australia / NZ', 110, true, ''),
  ('me',     'region', '中东站',   'Middle East', 120, true, ''),
  ('global', 'region', '全球可用', 'Globally reachable / multi-region', 130, true, '')
ON CONFLICT (id) DO NOTHING;

-- ========== 供给形态补充（全球常见计费形态浏览标签）==========
INSERT INTO market_categories (id, parent_id, name, description, sort, seller_pickable, icon) VALUES
  ('subscription', 'offer', '订阅额度', 'Subscription seat / monthly quota resale', 40, false, ''),
  ('payg',         'offer', '按量 API', 'Pay-as-you-go API key style (future)', 50, false, '')
ON CONFLICT (id) DO NOTHING;

-- ========== 运营场景补充（与 OpenRouter / 市场习惯对齐的可勾选标签）==========
INSERT INTO market_categories (id, parent_id, name, description, sort, seller_pickable, icon) VALUES
  ('multilingual', 'scene', '多语言',   'Strong multilingual / non-English', 90, true, ''),
  ('open_weight',  'scene', '开源权重', 'Open-weight / self-host lineage', 100, true, ''),
  ('enterprise',   'scene', '企业级',   'Enterprise / compliance oriented', 110, true, ''),
  ('newbie',       'scene', '新手友好', 'Beginner-friendly / low barrier', 120, true, ''),
  ('high_tpm',     'scene', '高吞吐',   'High TPM / throughput', 130, true, '')
ON CONFLICT (id) DO NOTHING;
