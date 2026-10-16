/**
 * 市场发现配置（原型）
 * 正式环境由管理后台 CRUD → API 下发；此处用 localStorage 模拟可改。
 * 字段：hot_searches / hot_bar / feed（与当前 Library 市场页对齐）
 * action：{ plat?, q?, tag?, kindTab? }
 */
window.MarketConfigDefaults = {
  version: 2,
  hot_searches: [
    { id: "hs_gpt", label: "ChatGPT Plus", enabled: true, sort: 10, action: { q: "ChatGPT" } },
    { id: "hs_claude", label: "Claude Max", enabled: true, sort: 20, action: { q: "Claude" } },
    { id: "hs_kimi", label: "Kimi Coding", enabled: true, sort: 30, action: { q: "Kimi" } },
    { id: "hs_low", label: "低倍率", enabled: true, sort: 40, action: { tag: "low_rate" } },
    { id: "hs_pool", label: "保障池", enabled: true, sort: 50, action: { plat: "保障池" } },
    { id: "hs_pack", label: "订阅套餐", enabled: true, sort: 60, action: { kindTab: "pack" } },
  ],
  hot_bar: { enabled: true },
  feed: { pin_pool_first: true },
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
    if (over.hot_searches) out.hot_searches = over.hot_searches;
    if (over.hot_bar) out.hot_bar = Object.assign({}, out.hot_bar, over.hot_bar);
    if (over.feed) out.feed = Object.assign({}, out.feed, over.feed);
    if (typeof over.version === "number") out.version = over.version;
    return out;
  },
  enabledSorted(list) {
    return (list || []).filter((x) => x.enabled !== false).sort((a, b) => (a.sort || 0) - (b.sort || 0));
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
};
