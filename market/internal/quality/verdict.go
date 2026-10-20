package quality

import (
	"math"
	"sort"
	"time"

	"github.com/yx-token/token-market/market/internal/inspection"
)

// Outcome 是一道题在被测上游与基线账号上的结果。
type Outcome struct {
	InstanceIdx       int
	GeneratorVersion  string
	PrimaryModel      bool
	StaticComparable  bool
	RequestedModel    string
	ProbeTokens       int
	ProbeOutputTokens int
	ProbeFeature      string
	ExpectedPassRate  *float64
	CheckKey          string
	ItemID            int64
	Suite             string
	Target            ProbeResult
	TargetPass        bool
	Ref               *ProbeResult
	RefPass           bool
	RefHistRate       *float64 // 该题在基线账号上近 7 天的通过率
}

// Assessment 是一次检测的评估结果。
type Assessment struct {
	InspectionMode       string                      `json:"inspection_mode,omitempty"`
	InspectionSettings   *InspectionSettings         `json:"inspection_settings,omitempty"`
	Checks               map[string]inspection.Check `json:"checks,omitempty"`
	Gates                []inspection.Gate           `json:"gates,omitempty"`
	QualityFactor        *float64                    `json:"quality_factor"`
	GroupFactors         map[string]float64          `json:"group_factors,omitempty"`
	Appraisal            *inspection.Appraisal       `json:"appraisal,omitempty"`
	Provisional          bool                        `json:"provisional,omitempty"`
	ProposedGrade        string                      `json:"proposed_grade,omitempty"`
	RepairedGrade        string                      `json:"repaired_grade,omitempty"`
	ErrorCounts          map[string]int              `json:"error_counts,omitempty"`
	ReferenceErrorCounts map[string]int              `json:"reference_error_counts,omitempty"`
	Warnings             []string                    `json:"warnings,omitempty"`
	Verdict              string                      `json:"verdict"` // normal / suspected / degraded / unavailable / inconclusive
	LLR                  float64                     `json:"llr,omitempty"`
	Samples              int                         `json:"samples,omitempty"`
	TargetPassRate       *float64                    `json:"target_pass_rate,omitempty"`
	RelativeScore        *float64                    `json:"relative_score,omitempty"`
	ReasoningRatio       *float64                    `json:"reasoning_ratio,omitempty"`
	Confidence           *float64                    `json:"confidence,omitempty"`
	Baseline             bool                        `json:"baseline"`
	AvailableOK          int                         `json:"available_ok"`
	AvailableTotal       int                         `json:"available_total"`
	TTFTp50Ms            *float64                    `json:"ttft_p50_ms,omitempty"`
	TTFTp95Ms            *float64                    `json:"ttft_p95_ms,omitempty"`
	OutputTPS            *float64                    `json:"output_tps,omitempty"`
	Grade                string                      `json:"grade,omitempty"`
}

// Assess 只汇总实际请求的可用率和性能；测智由检测项按模型判定。
// 保留旧调用参数，避免历史调用重新引入旧评分结论。
func Assess(outcomes []Outcome, _ Thresholds, _ bool) Assessment {
	a := Assessment{}
	var ttft, tps []float64
	for _, o := range outcomes {
		countProbeError(&a.ErrorCounts, o.Target.ErrorClass)
		if o.Ref != nil {
			countProbeError(&a.ReferenceErrorCounts, o.Ref.ErrorClass)
		}
		if o.Target.HTTPStatus == 0 && o.Target.ErrorClass == "" {
			continue
		}
		switch o.Target.ErrorClass {
		case "platform", "rate_limited", "expected_reject", "configuration":
			continue
		}
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
	switch {
	case a.AvailableTotal >= 2 && float64(a.AvailableOK)/float64(a.AvailableTotal) < 0.5:
		a.Verdict = "unavailable"
	case a.AvailableOK == 0:
		a.Verdict = "inconclusive"
	default:
		a.Verdict = "normal"
	}
	return a
}

func countProbeError(counts *map[string]int, class string) {
	// 预期的参数拒绝是有效指纹证据，不是待排查的探测故障。
	if class == "" || class == "expected_reject" {
		return
	}
	if *counts == nil {
		*counts = map[string]int{}
	}
	(*counts)[class]++
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
