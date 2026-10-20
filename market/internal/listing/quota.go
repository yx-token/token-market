package listing

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/yx-token/idlicat/market/internal/platform/httpx"
	"github.com/yx-token/idlicat/market/internal/sub2api/adminclient"
)

// QuotaWindow 是官方订阅的一个用量窗口。Utilization 由官方统计（包含卖家在本平台之外的使用），
// Requests / Tokens / Cost 是窗口内经本平台（sub2api）的使用量。
type QuotaWindow struct {
	Key           string     `json:"key"`   // five_hour / seven_day / seven_day_sonnet / seven_day_fable / thirty_day / gemini_*_daily / model:<名称> / cn:<窗口>
	Label         string     `json:"label"` // 中文名称
	Utilization   float64    `json:"utilization"`
	ResetsAt      *time.Time `json:"resets_at,omitempty"`
	Requests      *int64     `json:"requests,omitempty"`
	Tokens        *int64     `json:"tokens,omitempty"`
	Cost          *float64   `json:"cost,omitempty"`
	UsedRequests  int64      `json:"used_requests,omitempty"`
	LimitRequests int64      `json:"limit_requests,omitempty"`
}

// AccountQuota 是卖家看到的账号订阅额度。
type AccountQuota struct {
	Supported   bool          `json:"supported"`
	Source      string        `json:"source,omitempty"` // passive（响应头采样）/ active（向官方查询）/ coding_plan
	SampledAt   *time.Time    `json:"sampled_at,omitempty"`
	FetchedAt   time.Time     `json:"fetched_at"`
	PlanLevel   string        `json:"plan_level,omitempty"`
	Windows     []QuotaWindow `json:"windows"`
	Alerts      []string      `json:"alerts,omitempty"` // needs_reauth / banned / needs_verify / credential_invalid
	Error       string        `json:"error,omitempty"`
	SelfReserve *int          `json:"self_reserve_percent,omitempty"` // 支持自留比例的平台：窗口用量达到 100 − 自留比例 时暂停调度
}

var windowLabels = map[string]string{
	"five_hour":           "5 小时窗口",
	"seven_day":           "7 天窗口",
	"seven_day_sonnet":    "7 天窗口（Sonnet）",
	"seven_day_fable":     "7 天窗口（Opus）",
	"thirty_day":          "30 天窗口",
	"gemini_shared_daily": "每日共享额度",
	"gemini_pro_daily":    "每日 Pro 额度",
	"gemini_flash_daily":  "每日 Flash 额度",
}

type quotaCacheEntry struct {
	q      *AccountQuota
	at     time.Time
	active bool
}

// quotaCache 缓存每个上游最近一次查询结果，避免卖家频繁刷新打到官方接口。
var quotaCache = struct {
	sync.Mutex
	m map[int64]quotaCacheEntry
}{m: map[int64]quotaCacheEntry{}}

const (
	quotaCacheTTL   = time.Minute
	quotaRefreshGap = 2 * time.Minute // 主动查询官方的最小间隔
)

// AccountQuota 返回卖家上游的官方订阅窗口用量。Claude 默认读取 sub2api 的被动采样快照（不访问官方），
// refresh 为真或其他平台时由 sub2api 向官方查询；Coding Plan 读取各家的配额接口。
func (s *Service) AccountQuota(ctx context.Context, sellerID, upstreamID int64, refresh bool) (*AccountQuota, error) {
	u, err := GetSellerUpstream(ctx, s.DB, sellerID, upstreamID)
	if err != nil {
		return nil, err
	}
	if u.Sub2APIAccountID == nil {
		return nil, httpx.Conflict("NO_ACCOUNT", "the upstream has not been connected yet")
	}
	now := time.Now()
	quotaCache.Lock()
	cached, ok := quotaCache.m[u.ID]
	quotaCache.Unlock()
	if ok {
		fresh := now.Sub(cached.at) < quotaCacheTTL
		recentActive := cached.active && now.Sub(cached.at) < quotaRefreshGap
		if (fresh && !refresh) || recentActive {
			return s.withReserve(u, cached.q), nil
		}
	}
	q, active := s.fetchQuota(ctx, u, refresh)
	quotaCache.Lock()
	quotaCache.m[u.ID] = quotaCacheEntry{q: q, at: now, active: active}
	quotaCache.Unlock()
	return s.withReserve(u, q), nil
}

func (s *Service) withReserve(u *Upstream, q *AccountQuota) *AccountQuota {
	out := *q
	if SupportsSelfReserve(u.Platform) {
		v := u.SelfReservePercent
		out.SelfReserve = &v
	}
	return &out
}

func (s *Service) fetchQuota(ctx context.Context, u *Upstream, refresh bool) (*AccountQuota, bool) {
	q := &AccountQuota{FetchedAt: time.Now(), Windows: []QuotaWindow{}}
	if IsCodingAuthType(u.Platform, u.AuthType) {
		q.Supported, q.Source = true, "coding_plan"
		cn, err := s.Admin.CNProviderQuota(ctx, *u.Sub2APIAccountID)
		if err != nil {
			s.Log.Warn("coding plan quota", "upstream_id", u.ID, "err", err)
			q.Error = "暂时无法读取官方额度"
			return q, true
		}
		q.PlanLevel = cn.PlanLevel
		if !cn.CredentialValid {
			q.Alerts = append(q.Alerts, "credential_invalid")
		}
		if !cn.Success && cn.Error != "" {
			q.Error = cn.Error
		}
		for _, t := range cn.Tiers {
			w := QuotaWindow{Key: "cn:" + t.Window, Label: cnWindowLabel(t.Window), Utilization: t.UsedPercent}
			if ts, err := time.Parse(time.RFC3339, t.ResetAt); err == nil {
				w.ResetsAt = &ts
			}
			q.Windows = append(q.Windows, w)
		}
		return q, true
	}
	// Claude 的被动快照来自 sub2api 转发请求时官方返回的响应头，不额外访问官方
	passive := u.Platform == "anthropic" && !refresh
	usage, err := s.Admin.AccountUsage(ctx, *u.Sub2APIAccountID, passive)
	if err != nil && passive {
		passive = false
		usage, err = s.Admin.AccountUsage(ctx, *u.Sub2APIAccountID, false)
	}
	if err != nil {
		s.Log.Warn("account usage", "upstream_id", u.ID, "err", err)
		q.Error = "暂时无法读取官方额度"
		return q, !passive
	}
	q.Supported, q.Source, q.SampledAt, q.PlanLevel = true, usage.Source, usage.UpdatedAt, usage.SubscriptionTier
	if q.Source == "" {
		q.Source = map[bool]string{true: "passive", false: "active"}[passive]
	}
	for _, it := range []struct {
		key string
		w   *adminclient.UsageWindow
	}{
		{"five_hour", usage.FiveHour}, {"seven_day", usage.SevenDay}, {"seven_day_sonnet", usage.SevenDaySonnet},
		{"seven_day_fable", usage.SevenDayFable}, {"thirty_day", usage.ThirtyDay}, {"gemini_shared_daily", usage.GeminiSharedDaily},
		{"gemini_pro_daily", usage.GeminiProDaily}, {"gemini_flash_daily", usage.GeminiFlashDaily},
	} {
		if it.w == nil {
			continue
		}
		w := QuotaWindow{Key: it.key, Label: windowLabels[it.key], Utilization: it.w.Utilization, ResetsAt: it.w.ResetsAt,
			UsedRequests: it.w.UsedRequests, LimitRequests: it.w.LimitRequests}
		if st := it.w.WindowStats; st != nil {
			w.Requests, w.Tokens, w.Cost = &st.Requests, &st.Tokens, &st.Cost
		}
		q.Windows = append(q.Windows, w)
	}
	models := make([]string, 0, len(usage.AntigravityQuota))
	for m := range usage.AntigravityQuota {
		models = append(models, m)
	}
	sort.Strings(models)
	for _, m := range models {
		aq := usage.AntigravityQuota[m]
		w := QuotaWindow{Key: "model:" + m, Label: m, Utilization: float64(aq.Utilization)}
		if ts, err := time.Parse(time.RFC3339, aq.ResetTime); err == nil {
			w.ResetsAt = &ts
		}
		q.Windows = append(q.Windows, w)
	}
	if usage.NeedsReauth {
		q.Alerts = append(q.Alerts, "needs_reauth")
	}
	if usage.IsBanned {
		q.Alerts = append(q.Alerts, "banned")
	}
	if usage.NeedsVerify {
		q.Alerts = append(q.Alerts, "needs_verify")
	}
	if usage.Error != "" {
		q.Error = usage.Error
	}
	return q, !passive
}

func cnWindowLabel(w string) string {
	switch strings.ToLower(w) {
	case "5h":
		return "5 小时窗口"
	case "1d", "24h", "daily", "day":
		return "每日额度"
	case "7d", "weekly", "week":
		return "7 天窗口"
	case "30d", "monthly", "month":
		return "每月额度"
	}
	return fmt.Sprintf("%s 窗口", w)
}
