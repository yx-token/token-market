package quality

import (
	"math"
	"sort"
	"time"
)

// Outcome 是一道题在被测上游与基线账号上的结果。
type Outcome struct {
	ItemID      int64
	Suite       string
	Target      ProbeResult
	TargetPass  bool
	Ref         *ProbeResult
	RefPass     bool
	RefHistRate *float64 // 该题在基线账号上近 7 天的通过率
}

// Assessment 是一次检测的评估结果。
type Assessment struct {
	Verdict        string   `json:"verdict"` // normal / suspected / degraded / unavailable / inconclusive
	LLR            float64  `json:"llr"`
	Samples        int      `json:"samples"`
	TargetPassRate *float64 `json:"target_pass_rate,omitempty"`
	RelativeScore  *float64 `json:"relative_score,omitempty"`
	ReasoningRatio *float64 `json:"reasoning_ratio,omitempty"`
	Confidence     *float64 `json:"confidence,omitempty"`
	Baseline       bool     `json:"baseline"`
	AvailableOK    int      `json:"available_ok"`
	AvailableTotal int      `json:"available_total"`
	TTFTp50Ms      *float64 `json:"ttft_p50_ms,omitempty"`
	TTFTp95Ms      *float64 `json:"ttft_p95_ms,omitempty"`
	OutputTPS      *float64 `json:"output_tps,omitempty"`
	Grade          string   `json:"grade,omitempty"`
}

func scoringSuite(s string) bool { return s == "iq" || s == "reasoning" || s == "long_context" }

func clampP(p float64) float64 { return math.Min(0.99, math.Max(0.01, p)) }

// Assess 用序贯概率比检验（SPRT）判定是否降智：
// H0：被测通过率 = h0 × 基线通过率（正常）；H1：= h1 × 基线通过率（降智）。
// 每道题的基线通过率优先取该题近 7 天的基线历史，其次取本批次基线结果，没有基线时按 0.95 估计。
func Assess(outcomes []Outcome, th Thresholds, scoring bool) Assessment {
	a := Assessment{}
	upper := math.Log((1 - th.Beta) / th.Alpha)
	lower := math.Log(th.Beta / (1 - th.Alpha))

	var ttft, tps []float64
	var targetPass, refPass, both, targetScored int
	var tReason, rReason []float64
	for _, o := range outcomes {
		a.AvailableTotal++
		if !o.Target.OK() {
			continue
		}
		a.AvailableOK++
		if o.Target.TTFT > 0 {
			ttft = append(ttft, float64(o.Target.TTFT/time.Millisecond))
		}
		if v := o.Target.OutputTPS(); v > 0 {
			tps = append(tps, v)
		}
		if !scoring || !scoringSuite(o.Suite) {
			continue
		}
		var pRef float64
		refOK := o.Ref != nil && o.Ref.OK()
		switch {
		case o.RefHistRate != nil:
			pRef = *o.RefHistRate
			a.Baseline = true
		case refOK:
			a.Baseline = true
			if o.RefPass {
				pRef = 0.9
			} else {
				pRef = 0.3
			}
		case o.Ref != nil:
			continue // 基线本身失败，这道题不计入
		default:
			pRef = 0.95
		}
		p0, p1 := clampP(th.H0*pRef), clampP(th.H1*pRef)
		if o.TargetPass {
			a.LLR += math.Log(p1 / p0)
			targetPass++
		} else {
			a.LLR += math.Log((1 - p1) / (1 - p0))
		}
		targetScored++
		a.Samples++
		if refOK {
			both++
			if o.RefPass {
				refPass++
			}
		}
		if o.Suite == "reasoning" && refOK {
			t, r := reasoningAmount(o.Target), reasoningAmount(*o.Ref)
			if r > 0 {
				tReason = append(tReason, t)
				rReason = append(rReason, r)
			}
		}
	}
	if v, ok := percentile(ttft, 0.5); ok {
		a.TTFTp50Ms = &v
	}
	if v, ok := percentile(ttft, 0.95); ok {
		a.TTFTp95Ms = &v
	}
	if v, ok := percentile(tps, 0.5); ok {
		a.OutputTPS = &v
	}
	if targetScored > 0 {
		rate := float64(targetPass) / float64(targetScored)
		a.TargetPassRate = &rate
		var rel float64
		switch {
		case both > 0 && refPass > 0:
			rel = math.Min(1.5, rate/(float64(refPass)/float64(both)))
		case !a.Baseline:
			rel = math.Min(1.5, rate/0.95)
		default:
			rel = rate
		}
		a.RelativeScore = &rel
		a.Grade = gradeFor(rel)
	}
	if len(tReason) > 0 {
		t, _ := percentile(tReason, 0.5)
		r, _ := percentile(rReason, 0.5)
		if r > 0 {
			ratio := t / r
			a.ReasoningRatio = &ratio
		}
	}

	switch {
	case a.AvailableTotal >= 2 && float64(a.AvailableOK)/float64(a.AvailableTotal) < 0.5:
		a.Verdict = "unavailable"
	case !scoring || a.Samples == 0:
		a.Verdict = "normal"
		if a.AvailableOK == 0 {
			a.Verdict = "unavailable"
		}
	case a.LLR >= upper:
		a.Verdict = "degraded"
		c := 1 - th.Alpha
		a.Confidence = &c
	case a.LLR <= lower:
		a.Verdict = "normal"
		c := 1 - th.Beta
		a.Confidence = &c
	case a.LLR > 0:
		a.Verdict = "suspected"
	default:
		a.Verdict = "inconclusive"
	}
	return a
}

func reasoningAmount(r ProbeResult) float64 {
	if r.ReasoningTokens > 0 {
		return float64(r.ReasoningTokens)
	}
	// 没有推理 token 统计的协议（如 Claude）按思考内容长度估算，约 4 字符 1 token
	return float64(r.ThinkingChars) / 4
}

func gradeFor(rel float64) string {
	switch {
	case rel >= 0.97:
		return "S"
	case rel >= 0.9:
		return "A"
	case rel >= 0.8:
		return "B"
	case rel >= 0.7:
		return "C"
	default:
		return "D"
	}
}

func percentile(vals []float64, p float64) (float64, bool) {
	if len(vals) == 0 {
		return 0, false
	}
	s := append([]float64(nil), vals...)
	sort.Float64s(s)
	idx := int(math.Ceil(p*float64(len(s)))) - 1
	if idx < 0 {
		idx = 0
	}
	return s[idx], true
}
