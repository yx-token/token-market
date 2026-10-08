package scheduling

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/DueGin/token-market/internal/listing"
	"github.com/DueGin/token-market/internal/settings"
)

// 优先级段：回收段 1–19 严格优先于寄售段 50–79；不面向买家的账号放在 99。
const (
	priorityIdle         = 99
	buybackBandStart     = 1
	buybackBandEnd       = 19
	consignmentBandStart = 50
	consignmentBandEnd   = 79
	priceBucketWidth     = 0.05
)

type ListingInput struct {
	ID              int64
	Mode            string
	Status          string
	Multiplier      *float64
	JoinDefaultPool bool
	GroupID         *int64
}

type UpstreamInput struct {
	ID           int64
	Platform     string
	AuthType     string
	Status       string
	Health       string
	IsReference  bool
	AccountID    int64
	ProbeGroupID *int64
	Grade        string
	SelfReserve  int
	Listing      *ListingInput
	DefaultGrade string // 检测没有给出等级时使用的默认等级（厂商策略的 default_grade）
}

type VendorInput struct {
	DefaultConcurrency int
	ExtraLimits        map[string]any
}

type PlanContext struct {
	Pools      []Pool
	Vendors    map[string]VendorInput
	Commission float64
	Strategy   Strategy
}

// Desired 是一个上游在 sub2api 中的期望状态。
type Desired struct {
	UpstreamID       int64
	AccountID        int64
	BuyerFacing      bool
	GroupIDs         []int64
	Priority         int
	Concurrency      int
	LoadFactor       *int
	RateMultiplier   float64
	WindowThreshold  *int
	ExtraLimits      map[string]any
	Reason           string
	ListingGroupID   *int64
	ListingGroupRate *float64
}

// Strategy 决定上游在所在优先级段内的位置。第一期按回收看等级、寄售看价格；
// 后续可以替换为探索-利用、公平分配等策略。
type Strategy interface {
	Name() string
	BuybackPriority(u UpstreamInput, grade string) int
	ConsignmentPriority(u UpstreamInput, multiplier float64) int
}

// DefaultStrategy：回收段按等级（S→1, A→5, B→10, C→15, 其他→19），寄售段按每 0.05 一档的价格从低到高。
type DefaultStrategy struct{}

func (DefaultStrategy) Name() string { return "grade_first+price_bucket" }

func (DefaultStrategy) BuybackPriority(_ UpstreamInput, grade string) int {
	switch grade {
	case "S":
		return 1
	case "A":
		return 5
	case "B":
		return 10
	case "C":
		return 15
	default:
		return buybackBandEnd
	}
}

func (DefaultStrategy) ConsignmentPriority(_ UpstreamInput, m float64) int {
	bucket := int(math.Floor(m/priceBucketWidth + 1e-9)) // 加微小量避免 0.30/0.05 = 5.999… 的浮点误差
	p := consignmentBandStart + bucket
	if p > consignmentBandEnd {
		p = consignmentBandEnd
	}
	return p
}

// eligibleHealth 判断健康状态是否允许面向买家。unknown（尚未通过检测）一律不调度。
func eligibleHealth(health string) bool {
	switch health {
	case listing.HealthHealthy, listing.HealthLimited, listing.HealthSuspected:
		return true
	default:
		return false
	}
}

// Plan 是纯函数：根据上游、挂单与默认分组计算期望状态。
func Plan(pc PlanContext, ups []UpstreamInput) []Desired {
	strategy := pc.Strategy
	if strategy == nil {
		strategy = DefaultStrategy{}
	}
	out := make([]Desired, 0, len(ups))
	for _, u := range ups {
		v := pc.Vendors[u.Platform]
		d := Desired{
			UpstreamID:     u.ID,
			AccountID:      u.AccountID,
			Priority:       priorityIdle,
			Concurrency:    max(v.DefaultConcurrency, 1),
			RateMultiplier: 1,
			ExtraLimits:    v.ExtraLimits,
		}
		if u.ProbeGroupID != nil {
			d.GroupIDs = append(d.GroupIDs, *u.ProbeGroupID)
		}
		if listing.SupportsSelfReserve(u.Platform) {
			t := 100 - u.SelfReserve
			d.WindowThreshold = &t
		}
		l := u.Listing
		if l != nil && l.Multiplier != nil {
			d.RateMultiplier = *l.Multiplier
		}

		var reasons []string
		switch {
		case u.IsReference:
			reasons = append(reasons, "reference account")
		case u.Status != listing.StatusListed:
			reasons = append(reasons, "upstream status "+u.Status)
		case l == nil || l.Status != listing.ListingActive || l.Multiplier == nil:
			reasons = append(reasons, "no active listing")
		case !eligibleHealth(u.Health):
			reasons = append(reasons, "health "+u.Health)
		default:
			d.BuyerFacing = true
		}

		if d.BuyerFacing {
			m := *l.Multiplier
			grade := u.Grade
			if grade == "" {
				grade = u.DefaultGrade
			}
			for _, p := range pc.Pools {
				if !p.Accepts(u.Platform) || p.Sub2APIGroupID == nil {
					continue
				}
				if l.Mode == listing.ModeConsignment && !l.JoinDefaultPool {
					continue
				}
				if m > p.Cap()+1e-9 {
					reasons = append(reasons, fmt.Sprintf("multiplier above %s cap", p.Name))
					continue
				}
				d.GroupIDs = append(d.GroupIDs, *p.Sub2APIGroupID)
			}
			if l.GroupID != nil {
				d.GroupIDs = append(d.GroupIDs, *l.GroupID)
			}
			if l.Mode == listing.ModeBuyback {
				d.Priority = clamp(strategy.BuybackPriority(u, grade), buybackBandStart, buybackBandEnd)
				reasons = append(reasons, "buyback grade "+grade)
			} else {
				d.Priority = clamp(strategy.ConsignmentPriority(u, m), consignmentBandStart, consignmentBandEnd)
				reasons = append(reasons, fmt.Sprintf("consignment %.4f", m))
			}
			if u.Health == listing.HealthSuspected && d.Concurrency > 1 {
				d.Concurrency = (d.Concurrency + 1) / 2
				reasons = append(reasons, "suspected: concurrency halved")
			}
		}
		if l != nil && l.GroupID != nil && l.Mode == listing.ModeConsignment && l.Multiplier != nil {
			rate := listing.BuyerPrice(*l.Multiplier, pc.Commission)
			d.ListingGroupID = l.GroupID
			d.ListingGroupRate = &rate
		}
		d.GroupIDs = uniqueSorted(d.GroupIDs)
		d.Reason = strings.Join(reasons, "; ")
		out = append(out, d)
	}
	return out
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func uniqueSorted(ids []int64) []int64 {
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out := ids[:0]
	for i, id := range ids {
		if i == 0 || id != ids[i-1] {
			out = append(out, id)
		}
	}
	return out
}

// LoadPlanInputs 从本系统数据库读取规划所需的全部输入。
func LoadPlanInputs(ctx context.Context, db DB, st settings.Settings) (PlanContext, []UpstreamInput, error) {
	pc := PlanContext{
		Vendors:    map[string]VendorInput{},
		Commission: st.CommissionRate,
	}
	pools, err := ListPools(ctx, db, true)
	if err != nil {
		return pc, nil, err
	}
	pc.Pools = pools

	rows, err := db.Query(ctx, `SELECT platform, default_concurrency, extra_limits FROM vendor_configs`)
	if err != nil {
		return pc, nil, err
	}
	for rows.Next() {
		var p string
		var v VendorInput
		var extra []byte
		if err := rows.Scan(&p, &v.DefaultConcurrency, &extra); err != nil {
			rows.Close()
			return pc, nil, err
		}
		if err := json.Unmarshal(extra, &v.ExtraLimits); err != nil {
			rows.Close()
			return pc, nil, err
		}
		pc.Vendors[p] = v
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return pc, nil, err
	}

	// 默认等级取法与 quality.EffectivePolicy 一致：精确匹配接入方式的策略优先，其次厂商通用
	rows, err = db.Query(ctx, `SELECT u.id, u.platform, u.auth_type, u.status, u.health, u.is_reference, u.sub2api_account_id,
			u.probe_group_id, COALESCE(u.grade, ''), u.self_reserve_percent,
			l.id, l.mode, l.status, l.multiplier, l.join_default_pool, l.listing_group_id,
			COALESCE(p.default_grade, 'B')
		FROM upstreams u
		LEFT JOIN listings l ON l.upstream_id = u.id AND l.closed_at IS NULL
		LEFT JOIN LATERAL (SELECT dp.default_grade FROM detection_policies dp
			WHERE dp.platform = u.platform AND dp.auth_type IN (u.auth_type, '') ORDER BY (dp.auth_type = u.auth_type) DESC LIMIT 1) p ON true
		WHERE u.sub2api_account_id IS NOT NULL AND u.status IN ('verifying', 'verify_failed', 'ready', 'listed', 'suspended')
		ORDER BY u.id`)
	if err != nil {
		return pc, nil, err
	}
	defer rows.Close()
	var ups []UpstreamInput
	for rows.Next() {
		var u UpstreamInput
		var lid *int64
		var mode, status *string
		var mult *float64
		var join *bool
		var gid *int64
		if err := rows.Scan(&u.ID, &u.Platform, &u.AuthType, &u.Status, &u.Health, &u.IsReference, &u.AccountID,
			&u.ProbeGroupID, &u.Grade, &u.SelfReserve, &lid, &mode, &status, &mult, &join, &gid, &u.DefaultGrade); err != nil {
			return pc, nil, err
		}
		if lid != nil {
			u.Listing = &ListingInput{ID: *lid, Mode: *mode, Status: *status, Multiplier: mult, JoinDefaultPool: *join, GroupID: gid}
		}
		ups = append(ups, u)
	}
	return pc, ups, rows.Err()
}

// SaveDesired 把期望状态写入 upstream_runtime_params，供管理后台查看与执行器比对。
func SaveDesired(ctx context.Context, db DB, ds []Desired) error {
	for _, d := range ds {
		extra, err := json.Marshal(d.ExtraLimits)
		if err != nil {
			return err
		}
		var thr *int16
		if d.WindowThreshold != nil {
			v := int16(*d.WindowThreshold)
			thr = &v
		}
		if _, err := db.Exec(ctx, `INSERT INTO upstream_runtime_params
			(upstream_id, buyer_facing, group_ids, priority, concurrency, load_factor, rate_multiplier, window_threshold, extra_limits, reason, computed_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, now())
			ON CONFLICT (upstream_id) DO UPDATE SET buyer_facing = EXCLUDED.buyer_facing, group_ids = EXCLUDED.group_ids,
				priority = EXCLUDED.priority, concurrency = EXCLUDED.concurrency, load_factor = EXCLUDED.load_factor,
				rate_multiplier = EXCLUDED.rate_multiplier, window_threshold = EXCLUDED.window_threshold,
				extra_limits = EXCLUDED.extra_limits, reason = EXCLUDED.reason, computed_at = now()`,
			d.UpstreamID, d.BuyerFacing, d.GroupIDs, d.Priority, d.Concurrency, d.LoadFactor, d.RateMultiplier, thr, extra, d.Reason); err != nil {
			return err
		}
	}
	return nil
}
