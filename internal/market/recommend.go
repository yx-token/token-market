package market

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/DueGin/token-market/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
)

const recommendKey = "market_recommend"

// RecommendConfig 三层推荐运营配置：
//  1. Factors  — 可用打分因子目录
//  2. Profiles — 推荐类型（组合因子权重 + 候选池规则）
//  3. Tiles    — 首页格子（每格选用一种推荐类型）
type RecommendConfig struct {
	Version int `json:"version"`

	Factors  []FactorDef        `json:"factors"`
	Profiles []RecommendProfile `json:"profiles"`
	Tiles    []HomeTile         `json:"tiles"`

	MaxTiles       int  `json:"max_tiles"`
	HideEmptyTiles bool `json:"hide_empty_tiles"`

	// 旧版字段：仅用于读取迁移，保存时不再写出
	Slots                []RecommendSlot  `json:"slots,omitempty"`
	Supply               *SupplyWeights   `json:"supply,omitempty"`
	Demand               *DemandWeights   `json:"demand,omitempty"`
	Platform             *PlatformWeights `json:"platform,omitempty"`
	LowRateMaxMultiplier float64          `json:"low_rate_max_multiplier,omitempty"`
	PerTileSKU           int              `json:"per_tile_sku,omitempty"`
	MinSKUToShowTile     int              `json:"min_sku_to_show_tile,omitempty"`
	FreshDays            int              `json:"fresh_days,omitempty"`
	DiversityPerPlat     int              `json:"diversity_per_plat,omitempty"`
}

// FactorDef 因子目录项（系统内置，可启用/改默认权重与文案）。
type FactorDef struct {
	ID            string  `json:"id"` // price|grade|success|ttft|stock|fresh|explore|click|order|refund_pen
	Name          string  `json:"name"`
	Desc          string  `json:"desc"`
	Group         string  `json:"group"` // supply|demand|platform
	Enabled       bool    `json:"enabled"`
	DefaultWeight float64 `json:"default_weight"`
	Reserved      bool    `json:"reserved"` // 预留：尚无埋点，打分忽略
}

// RecommendProfile 推荐类型：一套因子权重 + 候选池规则。
type RecommendProfile struct {
	ID         string             `json:"id"`
	Name       string             `json:"name"`
	Desc       string             `json:"desc"`
	Enabled    bool               `json:"enabled"`
	FilterKind string             `json:"filter_kind"` // tag|kind|pool
	FilterVal  string             `json:"filter_val"`
	ActionTag  string             `json:"action_tag,omitempty"`
	ActionKind string             `json:"action_kind,omitempty"`
	ActionPlat string             `json:"action_plat,omitempty"`
	Weights    map[string]float64 `json:"weights"` // factor_id -> weight
	LowRateMax float64            `json:"low_rate_max_multiplier"`
	PerTileSKU int                `json:"per_tile_sku"`
	FreshDays  int                `json:"fresh_days"`
	Diversity  int                `json:"diversity_per_plat"`
	MinSKU     int                `json:"min_sku_to_show_tile"`
}

// HomeTile 首页格子：展示信息 + 选用哪种推荐类型。
type HomeTile struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	Desc      string  `json:"desc"`
	Eng       string  `json:"eng"`
	Theme     string  `json:"theme"`
	Enabled   bool    `json:"enabled"`
	Sort      int     `json:"sort"`
	Boost     float64 `json:"boost"`
	ProfileID string  `json:"profile_id"`
}

// 旧版兼容结构
type RecommendSlot struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	Desc       string  `json:"desc"`
	Eng        string  `json:"eng"`
	Theme      string  `json:"theme"`
	Enabled    bool    `json:"enabled"`
	Sort       int     `json:"sort"`
	Boost      float64 `json:"boost"`
	Kind       string  `json:"kind"`
	KindVal    string  `json:"kind_val"`
	ActionTag  string  `json:"action_tag,omitempty"`
	ActionKind string  `json:"action_kind,omitempty"`
	ActionPlat string  `json:"action_plat,omitempty"`
}

type SupplyWeights struct {
	Price   float64 `json:"price"`
	Grade   float64 `json:"grade"`
	Success float64 `json:"success"`
	TTFT    float64 `json:"ttft"`
	Stock   float64 `json:"stock"`
	Fresh   float64 `json:"fresh"`
}

type DemandWeights struct {
	Click     float64 `json:"click"`
	Order     float64 `json:"order"`
	RefundPen float64 `json:"refund_pen"`
}

type PlatformWeights struct {
	Explore float64 `json:"explore"`
}

func DefaultFactors() []FactorDef {
	return []FactorDef{
		{ID: "price", Name: "价格优势", Desc: "通道倍率越低越好；额度包面额适中略优", Group: "supply", Enabled: true, DefaultWeight: 1.2},
		{ID: "grade", Name: "质量等级", Desc: "检测等级 S>A>B>C", Group: "supply", Enabled: true, DefaultWeight: 1.0},
		{ID: "success", Name: "24h 成功率", Desc: "近 24 小时成功占比", Group: "supply", Enabled: true, DefaultWeight: 1.0},
		{ID: "ttft", Name: "首字延迟", Desc: "越快越好（>2s 近似 0）", Group: "supply", Enabled: true, DefaultWeight: 0.6},
		{ID: "stock", Name: "可售库存", Desc: "额度包剩余份数；通道中性分", Group: "supply", Enabled: true, DefaultWeight: 0.5},
		{ID: "fresh", Name: "新鲜度", Desc: "新上架窗口内线性衰减", Group: "supply", Enabled: true, DefaultWeight: 0.4},
		{ID: "explore", Name: "探索（抬新货）", Desc: "额外加在新鲜度上", Group: "platform", Enabled: true, DefaultWeight: 0.2},
		{ID: "click", Name: "点击热度", Desc: "预留：暂无埋点", Group: "demand", Enabled: false, DefaultWeight: 0, Reserved: true},
		{ID: "order", Name: "下单转化", Desc: "预留：暂无埋点", Group: "demand", Enabled: false, DefaultWeight: 0, Reserved: true},
		{ID: "refund_pen", Name: "退货惩罚", Desc: "预留：暂无埋点", Group: "demand", Enabled: false, DefaultWeight: 0, Reserved: true},
	}
}

func defaultWeightsFromFactors(factors []FactorDef) map[string]float64 {
	w := map[string]float64{}
	for _, f := range factors {
		if f.Enabled && !f.Reserved {
			w[f.ID] = f.DefaultWeight
		}
	}
	return w
}

func DefaultRecommend() RecommendConfig {
	factors := DefaultFactors()
	base := defaultWeightsFromFactors(factors)
	copyW := func(over map[string]float64) map[string]float64 {
		out := map[string]float64{}
		for k, v := range base {
			out[k] = v
		}
		for k, v := range over {
			out[k] = v
		}
		return out
	}
	return RecommendConfig{
		Version:        2,
		Factors:        factors,
		MaxTiles:       4,
		HideEmptyTiles: true,
		Profiles: []RecommendProfile{
			{
				ID: "algo_deal", Name: "低价优先", Desc: "偏便宜的按量通道", Enabled: true,
				FilterKind: "tag", FilterVal: "low_rate", ActionTag: "low_rate",
				Weights:    copyW(map[string]float64{"price": 1.6, "grade": 0.8}),
				LowRateMax: 0.55, PerTileSKU: 3, FreshDays: 7, Diversity: 2, MinSKU: 1,
			},
			{
				ID: "algo_code", Name: "写代码优选", Desc: "Coding 相关，偏稳偏快", Enabled: true,
				FilterKind: "tag", FilterVal: "coding", ActionTag: "coding",
				Weights:    copyW(map[string]float64{"success": 1.3, "ttft": 1.0, "grade": 1.1}),
				LowRateMax: 0.55, PerTileSKU: 3, FreshDays: 7, Diversity: 2, MinSKU: 1,
			},
			{
				ID: "algo_pool", Name: "保障池售价", Desc: "默认分组按售价从低到高", Enabled: true,
				FilterKind: "pool", FilterVal: "pool", ActionPlat: "保障池",
				Weights:    copyW(nil),
				LowRateMax: 0.55, PerTileSKU: 3, FreshDays: 7, Diversity: 0, MinSKU: 1,
			},
			{
				ID: "algo_pack", Name: "额度包推荐", Desc: "额度包库存与面额平衡", Enabled: true,
				FilterKind: "kind", FilterVal: "pack", ActionKind: "pack",
				Weights:    copyW(map[string]float64{"stock": 1.2, "price": 1.0, "fresh": 0.6}),
				LowRateMax: 0.55, PerTileSKU: 3, FreshDays: 7, Diversity: 2, MinSKU: 1,
			},
		},
		Tiles: []HomeTile{
			{ID: "deal", Title: "低倍率通道", Desc: "按量 · 官方价打折", Eng: "DEAL", Theme: "deal", Enabled: true, Sort: 10, Boost: 1, ProfileID: "algo_deal"},
			{ID: "code", Title: "写代码专场", Desc: "Coding Plan 热挂", Eng: "CODE", Theme: "code", Enabled: true, Sort: 20, Boost: 1, ProfileID: "algo_code"},
			{ID: "pool", Title: "保障池", Desc: "不挑卖家 · 自动派", Eng: "POOL", Theme: "pool", Enabled: true, Sort: 30, Boost: 1, ProfileID: "algo_pool"},
			{ID: "pack", Title: "额度包", Desc: "先买额度 · 可退货", Eng: "PACK", Theme: "buyer", Enabled: true, Sort: 40, Boost: 1, ProfileID: "algo_pack"},
		},
	}
}

func (c *RecommendConfig) normalize() {
	if c.Version < 2 || len(c.Profiles) == 0 {
		*c = migrateV1(*c)
	}
	if len(c.Factors) == 0 {
		c.Factors = DefaultFactors()
	} else {
		c.Factors = mergeFactors(DefaultFactors(), c.Factors)
	}
	if c.MaxTiles < 1 {
		c.MaxTiles = 4
	}
	if len(c.Tiles) == 0 {
		c.Tiles = DefaultRecommend().Tiles
	}
	for i := range c.Profiles {
		p := &c.Profiles[i]
		if p.Weights == nil {
			p.Weights = defaultWeightsFromFactors(c.Factors)
		}
		if p.PerTileSKU < 1 {
			p.PerTileSKU = 3
		}
		if p.LowRateMax <= 0 {
			p.LowRateMax = 0.55
		}
		syncProfileAction(p)
	}
	// 清理旧字段，避免再写出
	c.Slots = nil
	c.Supply = nil
	c.Demand = nil
	c.Platform = nil
	c.LowRateMaxMultiplier = 0
	c.PerTileSKU = 0
	c.MinSKUToShowTile = 0
	c.FreshDays = 0
	c.DiversityPerPlat = 0
	c.Version = 2
}

func mergeFactors(base, over []FactorDef) []FactorDef {
	byID := map[string]FactorDef{}
	for _, f := range over {
		byID[f.ID] = f
	}
	out := make([]FactorDef, 0, len(base))
	for _, b := range base {
		if o, ok := byID[b.ID]; ok {
			if o.Name != "" {
				b.Name = o.Name
			}
			if o.Desc != "" {
				b.Desc = o.Desc
			}
			b.Enabled = o.Enabled
			b.DefaultWeight = o.DefaultWeight
			// reserved 以系统为准
		}
		out = append(out, b)
	}
	return out
}

func syncProfileAction(p *RecommendProfile) {
	p.ActionTag, p.ActionKind, p.ActionPlat = "", "", ""
	switch p.FilterKind {
	case "tag":
		p.ActionTag = p.FilterVal
	case "kind":
		p.ActionKind = p.FilterVal
	case "pool":
		p.ActionPlat = "保障池"
	}
}

func migrateV1(old RecommendConfig) RecommendConfig {
	def := DefaultRecommend()
	if old.MaxTiles > 0 {
		def.MaxTiles = old.MaxTiles
	}
	def.HideEmptyTiles = true
	if old.Version >= 1 {
		def.HideEmptyTiles = old.HideEmptyTiles
	}

	w := defaultWeightsFromFactors(def.Factors)
	if old.Supply != nil {
		w["price"] = old.Supply.Price
		w["grade"] = old.Supply.Grade
		w["success"] = old.Supply.Success
		w["ttft"] = old.Supply.TTFT
		w["stock"] = old.Supply.Stock
		w["fresh"] = old.Supply.Fresh
	}
	if old.Platform != nil {
		w["explore"] = old.Platform.Explore
	}
	low := old.LowRateMaxMultiplier
	if low <= 0 {
		low = 0.55
	}
	per := old.PerTileSKU
	if per < 1 {
		per = 3
	}
	fresh := old.FreshDays
	if fresh == 0 && old.Version >= 1 {
		fresh = old.FreshDays
	}
	if fresh == 0 {
		fresh = 7
	}
	div := old.DiversityPerPlat
	if div == 0 && old.Version == 0 {
		div = 2
	}
	minSKU := old.MinSKUToShowTile
	if minSKU == 0 && old.Version == 0 {
		minSKU = 1
	}

	slots := old.Slots
	if len(slots) == 0 {
		return def
	}
	profiles := make([]RecommendProfile, 0, len(slots))
	tiles := make([]HomeTile, 0, len(slots))
	for _, s := range slots {
		pid := "algo_" + s.ID
		profiles = append(profiles, RecommendProfile{
			ID: pid, Name: s.Title + "算法", Desc: s.Desc, Enabled: true,
			FilterKind: s.Kind, FilterVal: s.KindVal,
			ActionTag: s.ActionTag, ActionKind: s.ActionKind, ActionPlat: s.ActionPlat,
			Weights:    copyFloatMap(w),
			LowRateMax: low, PerTileSKU: per, FreshDays: fresh, Diversity: div, MinSKU: minSKU,
		})
		tiles = append(tiles, HomeTile{
			ID: s.ID, Title: s.Title, Desc: s.Desc, Eng: s.Eng, Theme: s.Theme,
			Enabled: s.Enabled, Sort: s.Sort, Boost: s.Boost, ProfileID: pid,
		})
	}
	def.Profiles = profiles
	def.Tiles = tiles
	return def
}

func copyFloatMap(in map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (c RecommendConfig) Validate() error {
	if c.MaxTiles < 1 || c.MaxTiles > 8 {
		return errors.New("max_tiles must be 1–8")
	}
	factorIDs := map[string]bool{}
	for _, f := range c.Factors {
		if strings.TrimSpace(f.ID) == "" {
			return errors.New("factor id required")
		}
		factorIDs[f.ID] = true
	}
	profileIDs := map[string]bool{}
	for _, p := range c.Profiles {
		if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Name) == "" {
			return errors.New("profile id and name are required")
		}
		if profileIDs[p.ID] {
			return fmt.Errorf("duplicate profile id %q", p.ID)
		}
		profileIDs[p.ID] = true
		if p.PerTileSKU < 1 || p.PerTileSKU > 12 {
			return fmt.Errorf("profile %s: per_tile_sku must be 1–12", p.ID)
		}
		if p.LowRateMax <= 0 || p.LowRateMax > 2 {
			return fmt.Errorf("profile %s: low_rate_max_multiplier must be in (0, 2]", p.ID)
		}
		switch p.FilterKind {
		case "tag", "kind", "pool":
		default:
			return fmt.Errorf("profile %s: filter_kind must be tag|kind|pool", p.ID)
		}
		for fid, wv := range p.Weights {
			if !factorIDs[fid] {
				return fmt.Errorf("profile %s: unknown factor %q", p.ID, fid)
			}
			if wv < 0 || wv > 10 {
				return fmt.Errorf("profile %s: weight for %s must be 0–10", p.ID, fid)
			}
		}
	}
	tileIDs := map[string]bool{}
	for _, t := range c.Tiles {
		if strings.TrimSpace(t.ID) == "" || strings.TrimSpace(t.Title) == "" {
			return errors.New("tile id and title are required")
		}
		if tileIDs[t.ID] {
			return fmt.Errorf("duplicate tile id %q", t.ID)
		}
		tileIDs[t.ID] = true
		if t.Boost < 0 || t.Boost > 10 {
			return errors.New("tile boost must be 0–10")
		}
		if t.ProfileID == "" || !profileIDs[t.ProfileID] {
			return fmt.Errorf("tile %s: profile_id %q not found", t.ID, t.ProfileID)
		}
	}
	return nil
}

func profileByID(cfg RecommendConfig, id string) (RecommendProfile, bool) {
	for _, p := range cfg.Profiles {
		if p.ID == id {
			return p, true
		}
	}
	return RecommendProfile{}, false
}

// LoadRecommend 读取推荐配置；未设置则默认；旧版自动迁移。
func LoadRecommend(ctx context.Context, db discoveryDB) (RecommendConfig, error) {
	cfg := DefaultRecommend()
	var raw []byte
	err := db.QueryRow(ctx, `SELECT value FROM settings WHERE key = $1`, recommendKey).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	var loaded RecommendConfig
	if err := json.Unmarshal(raw, &loaded); err != nil {
		return DefaultRecommend(), err
	}
	loaded.normalize()
	return loaded, nil
}

// SaveRecommend 整份覆盖保存。
func SaveRecommend(ctx context.Context, db discoveryDB, cfg RecommendConfig) (RecommendConfig, error) {
	cfg.normalize()
	if err := cfg.Validate(); err != nil {
		return cfg, httpx.BadRequest("INVALID_RECOMMEND", err.Error())
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return cfg, err
	}
	_, err = db.Exec(ctx, `INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`, recommendKey, raw)
	if err != nil {
		return cfg, err
	}
	return LoadRecommend(ctx, db)
}

// ---- 打分结果 ----

type TileRec struct {
	ID        string       `json:"id"`
	Title     string       `json:"title"`
	Desc      string       `json:"desc"`
	Eng       string       `json:"eng"`
	Theme     string       `json:"theme"`
	Score     float64      `json:"score"`
	ProfileID string       `json:"profile_id,omitempty"`
	Action    TileAction   `json:"action"`
	Items     []TileSKURec `json:"items"`
}

type TileAction struct {
	Tag     string `json:"tag,omitempty"`
	KindTab string `json:"kindTab,omitempty"`
	Plat    string `json:"plat,omitempty"`
}

type TileSKURec struct {
	ListingID int64    `json:"listing_id,omitempty"`
	Key       string   `json:"key"`
	Title     string   `json:"title"`
	Plat      string   `json:"plat"`
	Price     string   `json:"price"`
	Score     float64  `json:"score"`
	Pool      bool     `json:"pool,omitempty"`
	Kind      string   `json:"kind"`
	FaceValue *float64 `json:"face_value,omitempty"`
	Mult      float64  `json:"mult,omitempty"`
}

type RecommendResult struct {
	ConfigVersion int       `json:"config_version"`
	GeneratedAt   time.Time `json:"generated_at"`
	Tiles         []TileRec `json:"tiles"`
}

const recommendCacheTTL = 45 * time.Second

// InvalidateRecommendCache 运营改推荐配置后清缓存。
func (s *Service) InvalidateRecommendCache() {
	s.recMu.Lock()
	s.recCache = nil
	s.recUntil = time.Time{}
	s.recMu.Unlock()
}

// CachedRecommendations 短时缓存首页四格，避免每次进站重扫挂单。
func (s *Service) CachedRecommendations(ctx context.Context, pools []PoolItem) (*RecommendResult, error) {
	now := time.Now()
	s.recMu.Lock()
	if s.recCache != nil && now.Before(s.recUntil) {
		out := s.recCache
		s.recMu.Unlock()
		return out, nil
	}
	s.recMu.Unlock()

	out, err := s.BuildRecommendations(ctx, pools)
	if err != nil {
		return nil, err
	}
	s.recMu.Lock()
	s.recCache = out
	s.recUntil = time.Now().Add(recommendCacheTTL)
	s.recMu.Unlock()
	return out, nil
}

// BuildRecommendations 按三层配置生成首页四格。
func (s *Service) BuildRecommendations(ctx context.Context, pools []PoolItem) (*RecommendResult, error) {
	cfg, err := LoadRecommend(ctx, s.DB)
	if err != nil {
		return nil, err
	}
	// 轻量列表：不做 24h 成功率/TTFT 侧向聚合（打分对空指标有默认值）
	items, err := s.ListForRecommend(ctx, 200)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	out := &RecommendResult{ConfigVersion: cfg.Version, GeneratedAt: now}

	type scoredTile struct {
		tile  HomeTile
		prof  RecommendProfile
		score float64
		skus  []TileSKURec
	}
	var candidates []scoredTile
	for _, tile := range cfg.Tiles {
		if !tile.Enabled {
			continue
		}
		prof, ok := profileByID(cfg, tile.ProfileID)
		if !ok || !prof.Enabled {
			continue
		}
		skus := pickSKUsForProfile(cfg, tile, prof, items, pools, now)
		tileScore := float64(100-tile.Sort) * math.Max(tile.Boost, 0.01)
		if len(skus) > 0 {
			sum := 0.0
			for _, x := range skus {
				sum += x.Score
			}
			tileScore += sum / float64(len(skus))
		} else if cfg.HideEmptyTiles {
			continue
		}
		if len(skus) < prof.MinSKU && cfg.HideEmptyTiles {
			continue
		}
		candidates = append(candidates, scoredTile{tile: tile, prof: prof, score: tileScore, skus: skus})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score == candidates[j].score {
			return candidates[i].tile.Sort < candidates[j].tile.Sort
		}
		return candidates[i].score > candidates[j].score
	})
	n := cfg.MaxTiles
	if n > len(candidates) {
		n = len(candidates)
	}
	for _, c := range candidates[:n] {
		out.Tiles = append(out.Tiles, TileRec{
			ID: c.tile.ID, Title: c.tile.Title, Desc: c.tile.Desc, Eng: c.tile.Eng, Theme: c.tile.Theme,
			Score: round2(c.score), ProfileID: c.prof.ID,
			Action: TileAction{Tag: c.prof.ActionTag, KindTab: c.prof.ActionKind, Plat: c.prof.ActionPlat},
			Items:  c.skus,
		})
	}
	if out.Tiles == nil {
		out.Tiles = []TileRec{}
	}
	return out, nil
}

func pickSKUsForProfile(cfg RecommendConfig, tile HomeTile, prof RecommendProfile, items []Item, pools []PoolItem, now time.Time) []TileSKURec {
	if prof.FilterKind == "pool" {
		return pickPools(prof, pools)
	}
	type row struct {
		it    Item
		score float64
	}
	var rows []row
	for _, it := range items {
		if !matchProfile(prof, it) {
			continue
		}
		rows = append(rows, row{it: it, score: scoreItemWithProfile(cfg, tile, prof, it, now)})
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].score > rows[j].score })
	perPlat := map[string]int{}
	limit := prof.PerTileSKU
	out := make([]TileSKURec, 0, limit)
	for _, r := range rows {
		if len(out) >= limit {
			break
		}
		if prof.Diversity > 0 && perPlat[r.it.Platform] >= prof.Diversity {
			continue
		}
		perPlat[r.it.Platform]++
		out = append(out, toSKURec(r.it, r.score))
	}
	return out
}

func pickPools(prof RecommendProfile, pools []PoolItem) []TileSKURec {
	sort.SliceStable(pools, func(i, j int) bool { return pools[i].Price < pools[j].Price })
	limit := prof.PerTileSKU
	if limit < 1 {
		limit = 3
	}
	out := make([]TileSKURec, 0, limit)
	for i, p := range pools {
		if len(out) >= limit {
			break
		}
		out = append(out, TileSKURec{
			Key: "pool-" + p.Platform + "-" + p.Name, Title: p.Name, Plat: p.Platform,
			Price: formatMult(p.Price), Score: float64(100 - i), Pool: true, Kind: "channel", Mult: p.Price,
		})
	}
	return out
}

func matchProfile(prof RecommendProfile, it Item) bool {
	switch prof.FilterKind {
	case "tag":
		switch prof.FilterVal {
		case "low_rate":
			if it.SKUKind == "pack" {
				return false
			}
			if hasCat(it.Categories, "low_rate") {
				return true
			}
			return it.BuyerPrice <= prof.LowRateMax+1e-9
		case "coding":
			if hasCat(it.Categories, "coding") {
				return true
			}
			switch it.Platform {
			case "kimi", "zhipu", "minimax", "opencode_go":
				return true
			}
			return false
		case "grade_s":
			return it.Grade != nil && *it.Grade == "S"
		case "featured":
			return hasCat(it.Categories, "featured")
		default:
			return hasCat(it.Categories, prof.FilterVal)
		}
	case "kind":
		return it.SKUKind == prof.FilterVal || (prof.FilterVal == "pack" && it.Mode == "pack")
	default:
		return false
	}
}

func weightOf(prof RecommendProfile, factors []FactorDef, id string) float64 {
	for _, f := range factors {
		if f.ID == id && (!f.Enabled || f.Reserved) {
			return 0
		}
	}
	if prof.Weights == nil {
		return 0
	}
	return prof.Weights[id]
}

func scoreItemWithProfile(cfg RecommendConfig, tile HomeTile, prof RecommendProfile, it Item, now time.Time) float64 {
	wPrice := weightOf(prof, cfg.Factors, "price")
	wGrade := weightOf(prof, cfg.Factors, "grade")
	wSuccess := weightOf(prof, cfg.Factors, "success")
	wTTFT := weightOf(prof, cfg.Factors, "ttft")
	wStock := weightOf(prof, cfg.Factors, "stock")
	wFresh := weightOf(prof, cfg.Factors, "fresh")
	wExplore := weightOf(prof, cfg.Factors, "explore")

	score := 0.0
	if it.SKUKind == "pack" || it.Mode == "pack" {
		face := 100.0
		if it.FaceValue != nil {
			face = *it.FaceValue
		}
		score += wPrice * (1.0 / (1.0 + math.Log1p(face/50)))
	} else {
		score += wPrice * clamp01((prof.LowRateMax*1.5-it.BuyerPrice)/(prof.LowRateMax*1.5))
	}
	score += wGrade * gradeScore(it.Grade)
	if it.SuccessRate24h != nil {
		score += wSuccess * clamp01(*it.SuccessRate24h)
	} else {
		score += wSuccess * 0.7
	}
	if it.TTFTp50Ms != nil && *it.TTFTp50Ms > 0 {
		score += wTTFT * clamp01(1.0-(*it.TTFTp50Ms)/2000.0)
	} else {
		score += wTTFT * 0.5
	}
	stockLeft := 0.8
	if it.Stock != nil {
		left := float64(*it.Stock - it.StockSold)
		if left < 0 {
			left = 0
		}
		stockLeft = clamp01(left / 10.0)
	}
	score += wStock * stockLeft
	ageDays := now.Sub(it.ListedSince).Hours() / 24
	fresh := 0.0
	if prof.FreshDays > 0 && ageDays >= 0 && ageDays <= float64(prof.FreshDays) {
		fresh = 1.0 - ageDays/float64(prof.FreshDays)
	}
	score += wFresh * fresh
	score += wExplore * fresh
	score *= math.Max(tile.Boost, 0.01)
	return score
}

func toSKURec(it Item, score float64) TileSKURec {
	kind := "channel"
	if it.SKUKind == "pack" || it.Mode == "pack" {
		kind = "pack"
	}
	price := formatMult(it.BuyerPrice)
	if kind == "pack" && it.FaceValue != nil {
		price = formatFace(*it.FaceValue)
	}
	title := ""
	if it.Title != nil {
		title = *it.Title
	}
	if title == "" {
		title = it.Platform
	}
	return TileSKURec{
		ListingID: it.ListingID, Key: "l-" + itoa(it.ListingID), Title: title, Plat: it.Platform,
		Price: price, Score: round2(score), Kind: kind, FaceValue: it.FaceValue, Mult: it.BuyerPrice,
	}
}

func hasCat(cats []string, id string) bool {
	for _, c := range cats {
		if c == id {
			return true
		}
	}
	return false
}

func gradeScore(g *string) float64 {
	if g == nil {
		return 0.4
	}
	switch *g {
	case "S":
		return 1
	case "A":
		return 0.85
	case "B":
		return 0.65
	case "C":
		return 0.4
	default:
		return 0.2
	}
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

func formatMult(m float64) string {
	return strconv.FormatFloat(m, 'f', 2, 64) + "×"
}

func formatFace(f float64) string {
	if f == math.Trunc(f) {
		return fmt.Sprintf("¥%.0f", f)
	}
	return fmt.Sprintf("¥%.2f", f)
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}
