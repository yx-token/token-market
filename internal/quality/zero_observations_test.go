package quality

import (
	"encoding/json"
	"testing"
)

func TestAssessKeepsExcludedErrorEvidence(t *testing.T) {
	var rows []Outcome
	for _, class := range []string{"platform", "platform", "rate_limited", "configuration", "expected_reject"} {
		reference := ProbeResult{ErrorClass: "platform"}
		rows = append(rows, Outcome{Suite: "iq", Target: ProbeResult{ErrorClass: class}, Ref: &reference})
	}
	a := Assess(rows, th, true)
	if a.AvailableTotal != 0 || a.Verdict != "inconclusive" || a.Samples != 0 {
		t.Fatalf("无有效观测不能得到质量结论：%+v", a)
	}
	if len(a.ErrorCounts) != 3 || a.ErrorCounts["platform"] != 2 || a.ErrorCounts["rate_limited"] != 1 || a.ErrorCounts["configuration"] != 1 || a.ReferenceErrorCounts["platform"] != 5 {
		t.Fatalf("排除观测的故障证据丢失：%+v", a)
	}
	e := Engine{}
	summary := e.buildSummary(runRow{trigger: "scheduled"}, &upstreamRow{}, a, "")
	var decoded struct {
		ErrorCounts          map[string]int `json:"error_counts"`
		ReferenceErrorCounts map[string]int `json:"reference_error_counts"`
		Suites               struct {
			Availability struct {
				Success int `json:"success"`
				Total   int `json:"total"`
			} `json:"availability"`
		} `json:"suites"`
	}
	if err := json.Unmarshal(mustJSON(summary), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ErrorCounts["platform"] != 2 || decoded.ReferenceErrorCounts["platform"] != 5 || decoded.Suites.Availability.Total != 0 {
		t.Fatalf("兼容摘要没有保存零观测与错误来源：%+v", decoded)
	}
}
