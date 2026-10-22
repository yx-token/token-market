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

// SelectSchedulePolicy 按路由形态选策略：含保障池 hop 时声明 pool-buyback-first
//（账号级回收优先由引擎 scheduling.Plan 执行；Edge 仍按 hop 有序故障转移）。
func SelectSchedulePolicy(hops []edgecfg.Hop) SchedulePolicy {
	for _, h := range hops {
		if h.PoolID > 0 {
			return PolicyPoolBuybackFirst
		}
	}
	return PolicyOrderedFailover
}

// BuildSchedulePlan 产出转发顺序。hop 顺序保持买家配置；Policy 字段标明契约策略。
func BuildSchedulePlan(policy SchedulePolicy, hops []edgecfg.Hop, skipped func(listingID int64) bool, allow func(label string) bool) SchedulePlan {
	if policy == "" {
		policy = SelectSchedulePolicy(hops)
	}
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
	// pool-buyback-first：不拆池内账号为多 hop（单内部 Key）；顺序与 ordered-failover 相同，
	// 引擎侧 buyback priority band 负责「优先吃回收」。见 edge-engine-contract.md。
	if policy != PolicyPoolBuybackFirst && policy != PolicyOrderedFailover {
		policy = PolicyOrderedFailover
	}
	return SchedulePlan{Policy: policy, Steps: steps}
}
