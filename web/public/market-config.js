/**
 * 市场发现配置（原型）
 * 正式环境由管理后台 CRUD → API 下发；此处用 localStorage 模拟可改。
 *
 * kind:
 *   category_group — 侧栏「闲鱼式」类目行（图标 + A/B/C + 悬停飞层）
 *   tag            — 标签（自动打标 / 筛选）
 *   slot           — 运营位（横幅/四宫格）
 *   hot_search     — 顶栏快捷标签
 *
 * action 跳转查询：{ plat?, q?, tag?, kindTab? }
 */
window.MarketConfigDefaults = {
  version: 2,
  /**
   * 侧栏类目组（对齐闲鱼：一行多个一级词，悬停展开二级/三级）
   * labels[].text 显示在侧栏，用「 / 」拼接
   * sections[] 飞层：title + items[]（点击跳转）
   */
  category_groups: [
    {
      id: "grp_intl",
      enabled: true,
      sort: 10,
      icon: "chat",
      labels: [
        { text: "Claude", action: { plat: "Claude" } },
        { text: "ChatGPT", action: { plat: "ChatGPT" } },
        { text: "Gemini", action: { plat: "Gemini" } },
      ],
      sections: [
        {
          title: "Claude",
          items: [
            { text: "全部 Claude", action: { plat: "Claude" } },
            { text: "Max 5x", action: { q: "Max 5x" } },
            { text: "Max 20x", action: { q: "Max 20x" } },
            { text: "Pro", action: { q: "Claude Pro" } },
            { text: "低倍率", action: { plat: "Claude", tag: "low_rate" } },
            { text: "额度包", action: { plat: "Claude", kindTab: "pack" } },
            { text: "保障池", action: { plat: "保障池", q: "Claude" } },
          ],
        },
        {
          title: "ChatGPT",
          items: [
            { text: "全部 ChatGPT", action: { plat: "ChatGPT" } },
            { text: "Plus", action: { q: "Plus" } },
            { text: "Team", action: { q: "Team" } },
            { text: "Codex", action: { q: "Codex" } },
            { text: "额度包", action: { plat: "ChatGPT", kindTab: "pack" } },
            { text: "保障池", action: { plat: "保障池", q: "ChatGPT" } },
          ],
        },
        {
          title: "Gemini",
          items: [
            { text: "全部 Gemini", action: { plat: "Gemini" } },
            { text: "Ultra", action: { q: "Ultra" } },
            { text: "Pro", action: { q: "Gemini Pro" } },
            { text: "额度包", action: { plat: "Gemini", kindTab: "pack" } },
          ],
        },
      ],
    },
    {
      id: "grp_coding",
      enabled: true,
      sort: 20,
      icon: "code",
      labels: [
        { text: "Kimi", action: { plat: "Kimi" } },
        { text: "智谱", action: { plat: "智谱" } },
        { text: "MiniMax", action: { plat: "MiniMax" } },
      ],
      sections: [
        {
          title: "写代码专场",
          items: [
            { text: "全部写代码", action: { tag: "coding" } },
            { text: "Kimi Coding", action: { plat: "Kimi" } },
            { text: "智谱 GLM", action: { plat: "智谱" } },
            { text: "MiniMax", action: { plat: "MiniMax" } },
            { text: "低倍率", action: { tag: "low_rate" } },
          ],
        },
        {
          title: "额度包",
          items: [
            { text: "Kimi 额度包", action: { plat: "Kimi", kindTab: "pack" } },
            { text: "智谱额度包", action: { plat: "智谱", kindTab: "pack" } },
            { text: "MiniMax 额度包", action: { plat: "MiniMax", kindTab: "pack" } },
          ],
        },
      ],
    },
    {
      id: "grp_deal",
      enabled: true,
      sort: 30,
      icon: "tag",
      labels: [
        { text: "按量", action: { kindTab: "channel" } },
        { text: "额度包", action: { kindTab: "pack" } },
        { text: "保障池", action: { plat: "保障池" } },
      ],
      sections: [
        {
          title: "闲置TOKEN类型",
          items: [
            { text: "按量通道", action: { kindTab: "channel" } },
            { text: "额度包", action: { kindTab: "pack" } },
            { text: "保障池", action: { plat: "保障池" } },
            { text: "全部闲置TOKEN", action: { kindTab: "all" } },
          ],
        },
        {
          title: "精选标签",
          items: [
            { text: "低倍率", action: { tag: "low_rate" } },
            { text: "S 级", action: { tag: "grade_s" } },
            { text: "写代码", action: { tag: "coding" } },
            { text: "可退货", action: { tag: "refundable" } },
            { text: "寄售", action: { tag: "consignment" } },
          ],
        },
      ],
    },
    {
      id: "grp_more",
      enabled: true,
      sort: 40,
      icon: "more",
      labels: [
        { text: "Grok", action: { plat: "Grok" } },
        { text: "其他", action: { q: "Antigravity" } },
        { text: "全部", action: { plat: "" } },
      ],
      sections: [
        {
          title: "其他厂商",
          items: [
            { text: "Grok", action: { plat: "Grok" } },
            { text: "Antigravity", action: { q: "Antigravity" } },
            { text: "全部厂商", action: { plat: "" } },
          ],
        },
        {
          title: "快捷入口",
          items: [
            { text: "我要发布闲置TOKEN", action: { href: "seller-center.html" } },
            { text: "市场配置", action: { href: "console.html#market" } },
          ],
        },
      ],
    },
  ],
  /** @deprecated 兼容旧扁平类目；新版以 category_groups 为准 */
  categories: [],
  tags: [
    {
      id: "consignment",
      name: "寄售",
      enabled: true,
      sort: 10,
      auto: { type: "sku_kind", value: "channel", exclude_pool: true },
      show_on_card: false,
    },
    {
      id: "low_rate",
      name: "低倍率",
      enabled: true,
      sort: 20,
      auto: { type: "mult_lte", value: 0.55 },
      show_on_card: true,
      hot: true,
    },
    {
      id: "grade_s",
      name: "S级",
      enabled: true,
      sort: 30,
      auto: { type: "grade_eq", value: "S" },
      show_on_card: true,
    },
    {
      id: "coding",
      name: "写代码",
      enabled: true,
      sort: 40,
      auto: { type: "platform_in", value: ["Kimi", "智谱", "MiniMax", "OpenCode"] },
      show_on_card: true,
      hot: true,
      slot: "tile_code",
    },
    {
      id: "pool",
      name: "保障",
      enabled: true,
      sort: 50,
      auto: { type: "is_pool", value: true },
      show_on_card: true,
      hot: true,
      slot: "tile_pool",
    },
    {
      id: "pack",
      name: "额度包",
      enabled: true,
      sort: 60,
      auto: { type: "sku_kind", value: "pack" },
      show_on_card: true,
      hot: true,
      slot: "tile_pack",
    },
    {
      id: "refundable",
      name: "可退货",
      enabled: true,
      sort: 70,
      auto: { type: "sku_kind", value: "pack" },
      show_on_card: true,
    },
    {
      id: "low_stock",
      name: "仅剩少量",
      enabled: true,
      sort: 80,
      auto: { type: "stock_lte", value: 2 },
      show_on_card: true,
    },
  ],
  slots: [
    {
      id: "banner",
      name: "主横幅",
      enabled: true,
      title: "按量通道\n选用即开通",
      desc: "起步金可 0 · 用多少扣多少",
      action: { kind: "filter", kindTab: "channel" },
    },
    {
      id: "tile_deal",
      name: "四宫格·低倍率",
      enabled: true,
      title: "低倍率通道",
      desc: "按量 · 官方价打折",
      action: { kind: "filter", tag: "low_rate" },
      eng: "DEAL",
      theme: "deal",
    },
    {
      id: "tile_code",
      name: "四宫格·写代码",
      enabled: true,
      title: "写代码专场",
      desc: "Coding Plan 热挂",
      action: { kind: "filter", tag: "coding" },
      eng: "CODE",
      theme: "code",
    },
    {
      id: "tile_pool",
      name: "四宫格·保障池",
      enabled: true,
      title: "保障池",
      desc: "不挑卖家 · 自动派",
      action: { kind: "filter", plat: "保障池" },
      eng: "POOL",
      theme: "pool",
    },
    {
      id: "tile_pack",
      name: "四宫格·额度包",
      enabled: true,
      title: "额度包",
      desc: "先买额度 · 可退货",
      action: { kind: "filter", kindTab: "pack" },
      eng: "PACK",
      theme: "buyer",
    },
  ],
  /** 市场顶栏快捷标签（可配置显示/排序） */
  hot_searches: [
    { id: "hs_claude", label: "Claude Max", enabled: true, sort: 10, action: { q: "Claude" } },
    { id: "hs_gpt", label: "ChatGPT Plus", enabled: true, sort: 20, action: { q: "ChatGPT" } },
    { id: "hs_kimi", label: "Kimi Coding", enabled: true, sort: 30, action: { q: "Kimi" } },
    { id: "hs_low", label: "低倍率", enabled: true, sort: 40, action: { tag: "low_rate" } },
    { id: "hs_pool", label: "保障池", enabled: true, sort: 50, action: { plat: "保障池" } },
    { id: "hs_pack", label: "额度包", enabled: true, sort: 60, action: { kindTab: "pack" } },
    { id: "hs_code", label: "写代码", enabled: true, sort: 70, action: { tag: "coding" } },
  ],
  /** 顶栏是否显示快捷标签条 */
  hot_bar: { enabled: true },
  feed: {
    pin_pool_first: true,
    pin_ops_ids: [],
  },
};

window.MarketConfigStore = {
  KEY: "xianmao_market_config_v2",
  load() {
    try {
      const raw = localStorage.getItem(this.KEY);
      if (raw) {
        const parsed = JSON.parse(raw);
        return this.merge(window.MarketConfigDefaults, parsed);
      }
    } catch (_) {}
    return JSON.parse(JSON.stringify(window.MarketConfigDefaults));
  },
  save(cfg) {
    localStorage.setItem(this.KEY, JSON.stringify(cfg));
  },
  reset() {
    localStorage.removeItem(this.KEY);
    return this.load();
  },
  merge(base, over) {
    const out = JSON.parse(JSON.stringify(base));
    if (over.category_groups) out.category_groups = over.category_groups;
    if (over.categories) out.categories = over.categories;
    if (over.tags) out.tags = over.tags;
    if (over.slots) out.slots = over.slots;
    if (over.hot_searches) out.hot_searches = over.hot_searches;
    if (over.hot_bar) out.hot_bar = Object.assign({}, out.hot_bar, over.hot_bar);
    if (over.feed) out.feed = Object.assign({}, out.feed, over.feed);
    return out;
  },
  enabledSorted(list) {
    return (list || []).filter((x) => x.enabled !== false).sort((a, b) => (a.sort || 0) - (b.sort || 0));
  },
  /** 侧栏图标 SVG（按 icon key） */
  catIconSvg(key) {
    const icons = {
      chat: '<svg viewBox="0 0 24 24" fill="none"><rect x="3.5" y="5" width="17" height="12" rx="2.5" stroke="#1f1f1f" stroke-width="1.5"/><path d="M8 17.5L6 20.5V17.5" stroke="#1f1f1f" stroke-width="1.5" stroke-linejoin="round"/><circle cx="9" cy="11" r="1" fill="#FFE60F"/><circle cx="12" cy="11" r="1" fill="#1f1f1f"/><circle cx="15" cy="11" r="1" fill="#FF6A3D"/></svg>',
      code: '<svg viewBox="0 0 24 24" fill="none"><path d="M9 8.5L5.5 12 9 15.5M15 8.5L18.5 12 15 15.5" stroke="#1f1f1f" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"/><path d="M13 7l-2 10" stroke="#3D8BFF" stroke-width="1.5" stroke-linecap="round"/></svg>',
      tag: '<svg viewBox="0 0 24 24" fill="none"><path d="M4 12.5V5.8A1.8 1.8 0 0 1 5.8 4H12l8 8-7.2 7.2a1.8 1.8 0 0 1-2.5 0L4 12.5z" stroke="#1f1f1f" stroke-width="1.5" stroke-linejoin="round"/><circle cx="9" cy="9" r="1.2" fill="#FFE60F" stroke="#1f1f1f" stroke-width="1"/></svg>',
      more: '<svg viewBox="0 0 24 24" fill="none"><circle cx="12" cy="12" r="8" stroke="#1f1f1f" stroke-width="1.5"/><circle cx="8.5" cy="12" r="1.1" fill="#1f1f1f"/><circle cx="12" cy="12" r="1.1" fill="#FFE60F"/><circle cx="15.5" cy="12" r="1.1" fill="#1f1f1f"/></svg>',
      list: '<svg viewBox="0 0 24 24" fill="none" stroke="#1f1f1f" stroke-width="1.6"><path d="M4 6h16M4 12h16M4 18h10"/></svg>',
    };
    return icons[key] || icons.list;
  },
  actionToAttrs(action) {
    const a = action || {};
    let attrs = 'href="#feed"';
    if (a.href) attrs = 'href="' + a.href + '"';
    if (a.q) attrs += ' data-q="' + String(a.q).replace(/"/g, "&quot;") + '"';
    if (a.tag) attrs += ' data-tag="' + a.tag + '"';
    if (a.plat !== undefined) attrs += ' data-plat="' + String(a.plat).replace(/"/g, "&quot;") + '"';
    if (a.kindTab) attrs += ' data-kind="' + a.kindTab + '"';
    if (a.href) attrs += ' data-href="1"';
    return attrs;
  },
  attrsToAction(el) {
    if (!el) return null;
    if (el.dataset.href === "1") return { href: el.getAttribute("href") };
    const action = {};
    if (el.dataset.kind) action.kindTab = el.dataset.kind;
    if (el.hasAttribute("data-plat")) action.plat = el.dataset.plat;
    if (el.dataset.tag) action.tag = el.dataset.tag;
    if (el.dataset.q) action.q = el.dataset.q;
    return action;
  },
  /** 根据自动规则给 SKU 打标 */
  resolveTags(sku, cfg) {
    const tags = (cfg || this.load()).tags.filter((t) => t.enabled !== false);
    const hit = [];
    for (const t of tags) {
      const a = t.auto || {};
      let ok = false;
      if (a.type === "sku_kind") {
        ok = sku.kind === a.value && !(a.exclude_pool && sku.pool);
      } else if (a.type === "is_pool") {
        ok = !!sku.pool === !!a.value;
      } else if (a.type === "mult_lte") {
        ok = sku.kind === "channel" && typeof sku.mult === "number" && sku.mult <= a.value;
      } else if (a.type === "grade_eq") {
        ok = sku.grade === a.value;
      } else if (a.type === "platform_in") {
        ok = (a.value || []).includes(sku.plat);
      } else if (a.type === "stock_lte") {
        ok = sku.kind === "pack" && typeof sku.stock === "number" && sku.stock <= a.value;
      } else if (a.type === "manual") {
        ok = (sku.manual_tags || []).includes(t.id);
      }
      if (ok) hit.push(t);
    }
    hit.sort((x, y) => (x.sort || 0) - (y.sort || 0));
    return hit;
  },
};
