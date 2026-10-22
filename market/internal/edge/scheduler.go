package edge

import "github.com/yx-token/token-market/market/internal/edgecfg"

// SchedulePolicy 标识 Edge 调度策略；对买家可观测为 X-TM-Schedule-Policy。
// 见 docs/dev/edge-engine-contract.md。
type SchedulePolicy string

const (
	// PolicyOrderedFailover 按 Key 上 hops 顺序尝试（当前默认）。
	PolicyOrderedFailover SchedulePolicy = "ordered-failover"
	// PolicyPoolBuybackFirst 保障池优先吃回收产能（P2；BuildSchedulePlan 尚未选用）。
	PolicyPoolBuybackFirst SchedulePolicy = "pool-buyback-first"
)

// ScheduleStep 是计划中的一跳：可能直接标记跳过（仍记 attempt），或进入转发。
type ScheduleStep struct {
	Index      int // cfg.Hops 下标
	Hop        edgecfg.Hop
	SkipReason string // "" | "skip_list" | "breaker"
}

// SchedulePlan 是一次请求的调度结果。
type SchedulePlan struct {
	Policy SchedulePolicy
	Steps  []ScheduleStep
}

// BuildSchedulePlan 产出转发顺序。P1 仅实现 ordered-failover；P2 在此分支其它 Policy。
func BuildSchedulePlan(policy SchedulePolicy, hops []edgecfg.Hop, skipped func(listingID int64) bool, allow func(label string) bool) SchedulePlan {
	if policy == "" {
		policy = PolicyOrderedFailover
	}
	// P2：policy == PolicyPoolBuybackFirst 时重排/过滤；现回落到有序故障转移以保持安全默认。
	_ = PolicyPoolBuybackFirst
	steps := make([]ScheduleStep, 0, len(hops))
	for i, hop := range hops {
		st := ScheduleStep{Index: i, Hop: hop}
		switch {
		case hop.ListingID > 0 && skipped != nil && skipped(hop.ListingID):
			st.SkipReason = "skip_list"
		case allow != nil && !allow(hop.Label):
			st.SkipReason = "breaker"
		}
		steps = append(steps, st)
	}
	return SchedulePlan{Policy: PolicyOrderedFailover, Steps: steps}
}
