package listing

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/yx-token/token-market/market/internal/inspection"
)

// ModelDetection 仅向买家披露检测状态，不暴露分数、阈值、提示词或内部模型证据。
type ModelDetection struct {
	Model             string `json:"model"`
	DetectionStatus   string `json:"detection_status"`
	DegradationStatus string `json:"degradation_status,omitempty"`
}

func detectionStatus(at *time.Time, checks map[string]inspection.Check) string {
	if at != nil {
		for _, c := range checks {
			if observedCheck(c.Status, max(c.Samples.Observed, c.Samples.Paired)-c.Samples.Excluded) {
				return "tested"
			}
		}
	}
	return "untested"
}

func observedCheck(status string, samples int) bool {
	if samples <= 0 {
		return false
	}
	switch status {
	case "pass", "warning", "fail", "inconclusive":
		return true
	default:
		return false
	}
}

func modelDetections(at *time.Time, models []string, checks map[string]inspection.Check) []ModelDetection {
	out := make([]ModelDetection, 0, len(models))
	for _, model := range models {
		v := ModelDetection{Model: model, DetectionStatus: "untested"}
		if model == "gpt-6-astra" {
			v.DegradationStatus = "untested"
		}
		if at == nil {
			out = append(out, v)
			continue
		}
		for key, check := range checks {
			for _, metric := range check.Metrics {
				for _, result := range metric.Models {
					if result.Model != model || !observedCheck(result.Status, result.Samples) {
						continue
					}
					v.DetectionStatus = "tested"
					if key == "D14" && model == "gpt-6-astra" {
						switch result.Status {
						case "pass":
							v.DegradationStatus = "passed"
						case "fail":
							v.DegradationStatus = "failed"
						default:
							v.DegradationStatus = "inconclusive"
						}
					}
				}
			}
		}
		out = append(out, v)
	}
	return out
}

// currentDetectionChecks 使用与检测引擎相同的逐项定义比较和证据有效期。
// 缺少冻结设置时不能把存量默认值或孤立 verdict 解释为已检测。
func currentDetectionChecks(checks map[string]inspection.Check, snapshot []byte, current inspection.Settings, reset *time.Time, now time.Time) map[string]inspection.Check {
	var saved struct {
		Settings *inspection.Settings `json:"inspection_settings"`
		Policy   struct {
			Flows map[string]struct {
				IntervalMinutes int `json:"interval_minutes"`
			} `json:"flows"`
		} `json:"policy"`
	}
	out := map[string]inspection.Check{}
	if json.Unmarshal(snapshot, &saved) != nil || saved.Settings == nil {
		return out
	}
	for key, check := range checks {
		definition, known := inspection.Effective(key, current)
		prior, wasKnown := inspection.Effective(key, *saved.Settings)
		if !known || !wasKnown || !definition.Enabled {
			continue
		}
		currentJSON, _ := json.Marshal(definition)
		priorJSON, _ := json.Marshal(prior)
		if !bytes.Equal(currentJSON, priorJSON) {
			continue
		}
		ttl := max(7*24*time.Hour, 2*time.Duration(saved.Policy.Flows[definition.Flow].IntervalMinutes)*time.Minute)
		if check.CheckedAt.IsZero() || check.CheckedAt.After(now) || now.Sub(check.CheckedAt) > ttl || (reset != nil && !check.CheckedAt.After(*reset)) {
			continue
		}
		out[key] = check
	}
	return out
}
