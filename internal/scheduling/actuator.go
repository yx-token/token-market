package scheduling

import (
	"fmt"
	"math"
	"sort"

	"github.com/DueGin/token-market/internal/sub2api/adminclient"
	"github.com/DueGin/token-market/internal/sub2api/opshandlers"
	"github.com/DueGin/token-market/internal/sub2api/replica"
)

// Actual 是从 sub2api 只读库读到的实际状态。
type Actual struct {
	Accounts map[int64]replica.Account // account_id → 账号
	Groups   map[int64][]int64         // account_id → 所属分组（升序）
	GroupRow map[int64]replica.Group   // group_id → 分组
}

// PlannedOp 是执行器要入队的一个出站操作。
type PlannedOp struct {
	Type     string
	Target   string
	Payload  any
	Urgent   bool
	Upstream int64
	// AppliedThreshold 非空时，入队成功后记录为已下发的窗口阈值
	AppliedThreshold *int
}

// DiffInput 是执行器一轮比对所需的全部输入。
type DiffInput struct {
	Desired          []Desired
	Actual           Actual
	Pending          map[string]bool // 仍有未完成操作的 target
	AppliedThreshold map[int64]*int  // upstream_id → 最近一次下发的窗口阈值
	Budget           int             // 本轮最多改动的账号数
}

// Diff 比较期望与实际状态，生成需要下发的操作。纯函数，便于测试。
// 同一账号仍有未完成操作时本轮跳过，等上一批生效后再比对，避免重复下发。
func Diff(in DiffInput) (ops []PlannedOp, missing []int64) {
	type candidate struct {
		urgent bool
		ops    []PlannedOp
	}
	var cands []candidate
	for _, d := range in.Desired {
		a, ok := in.Actual.Accounts[d.AccountID]
		if !ok {
			missing = append(missing, d.UpstreamID)
			continue
		}
		target := fmt.Sprintf("account:%d", d.AccountID)
		if in.Pending[target] {
			continue
		}
		var c candidate

		upd := adminclient.UpdateAccountInput{}
		changed := false
		actualGroups := in.Actual.Groups[d.AccountID]
		if !equalIDs(actualGroups, d.GroupIDs) {
			g := append([]int64(nil), d.GroupIDs...)
			upd.GroupIDs = &g
			changed = true
			// 从买家可用的分组中移出是安全相关操作，优先发送
			if !d.BuyerFacing && len(actualGroups) > len(d.GroupIDs) {
				c.urgent = true
			}
		}
		if a.Priority != d.Priority {
			p := d.Priority
			upd.Priority = &p
			changed = true
		}
		if a.Concurrency != d.Concurrency {
			cc := d.Concurrency
			upd.Concurrency = &cc
			changed = true
		}
		if math.Abs(a.RateMultiplier-d.RateMultiplier) > 1e-6 {
			r := d.RateMultiplier
			upd.RateMultiplier = &r
			changed = true
		}
		if d.LoadFactor != nil && (a.LoadFactor == nil || *a.LoadFactor != *d.LoadFactor) {
			lf := *d.LoadFactor
			upd.LoadFactor = &lf
			changed = true
		}
		if changed {
			c.ops = append(c.ops, PlannedOp{
				Type: opshandlers.TypeAccountUpdate, Target: target, Urgent: c.urgent, Upstream: d.UpstreamID,
				Payload: opshandlers.AccountUpdatePayload{AccountID: d.AccountID, Input: upd},
			})
		}

		bulk := adminclient.BulkUpdateAccountsInput{AccountIDs: []int64{d.AccountID}}
		bulkChanged := false
		extra := map[string]any{}
		for k, want := range d.ExtraLimits {
			if !sameJSONValue(a.Extra[k], want) {
				extra[k] = want
			}
		}
		if len(extra) > 0 {
			bulk.Extra = extra
			bulkChanged = true
		}
		var appliedThr *int
		if d.WindowThreshold != nil {
			prev := in.AppliedThreshold[d.UpstreamID]
			if prev == nil || *prev != *d.WindowThreshold {
				bulk.Credentials = map[string]any{"account_scheduling_threshold": *d.WindowThreshold}
				t := *d.WindowThreshold
				appliedThr = &t
				bulkChanged = true
			}
		}
		if bulkChanged {
			c.ops = append(c.ops, PlannedOp{
				Type: opshandlers.TypeAccountBulkUpdate, Target: target, Urgent: c.urgent, Upstream: d.UpstreamID,
				Payload: bulk, AppliedThreshold: appliedThr,
			})
		}

		if d.ListingGroupID != nil && d.ListingGroupRate != nil {
			gtarget := fmt.Sprintf("group:%d", *d.ListingGroupID)
			if g, ok := in.Actual.GroupRow[*d.ListingGroupID]; ok && !in.Pending[gtarget] && math.Abs(g.RateMultiplier-*d.ListingGroupRate) > 1e-6 {
				rate := *d.ListingGroupRate
				c.ops = append(c.ops, PlannedOp{
					Type: opshandlers.TypeGroupUpdate, Target: gtarget, Upstream: d.UpstreamID,
					Payload: opshandlers.GroupUpdatePayload{GroupID: *d.ListingGroupID, Input: adminclient.UpdateGroupInput{RateMultiplier: &rate}},
				})
			}
		}
		if len(c.ops) > 0 {
			cands = append(cands, c)
		}
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].urgent && !cands[j].urgent })
	budget := in.Budget
	if budget <= 0 {
		budget = len(cands)
	}
	for i, c := range cands {
		// 紧急操作不受预算限制
		if i >= budget && !c.urgent {
			break
		}
		ops = append(ops, c.ops...)
	}
	return ops, missing
}

func equalIDs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// sameJSONValue 比较 sub2api extra 中的值与期望值（数字统一按 float64 比较）。
func sameJSONValue(actual, want any) bool {
	af, aok := toFloat(actual)
	wf, wok := toFloat(want)
	if aok && wok {
		return math.Abs(af-wf) < 1e-9
	}
	return fmt.Sprint(actual) == fmt.Sprint(want) && actual != nil
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	}
	return 0, false
}
