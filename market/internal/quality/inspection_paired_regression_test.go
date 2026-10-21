package quality

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/yx-token/token-market/market/internal/inspection"
)

type pairedRegressionResult struct {
	Checks map[string]inspection.Check `json:"checks"`
	Gates  []inspection.Gate           `json:"gates"`
	Formal Assessment                  `json:"formal"`
}

type pairedRegressionCase struct {
	name string
	rows []Outcome
	st   InspectionSettings
}

func pairedRegressionCases() []pairedRegressionCase {
	var cases []pairedRegressionCase
	for _, name := range []string{"正常配对", "差异配对", "基线未兑现", "部分证据", "未校准", "禁用功能"} {
		st := calibratedInspectionSettings()
		var rows []Outcome
		for _, key := range []string{"D07", "D07.honesty", "D09", "A11.tools", "A11.json_schema", "A11.multiturn", "A11.image_pdf", "A11.thinking", "A04", "A05", "A15", "D08", "D12"} {
			for i := 0; i < 30; i++ {
				o := pairedInspectionOutcome(key, true, true)
				o.ItemID, o.GeneratorVersion = int64(i), "math/1"
				o.Target.InputTokens, o.Ref.InputTokens = int64Pointer(100), int64Pointer(100)
				o.Target.TTFT, o.Ref.TTFT = time.Second, time.Second
				o.Target.Duration, o.Ref.Duration = 2*time.Second, 2*time.Second
				o.Target.OutputTokens, o.Ref.OutputTokens = 100, 100
				switch name {
				case "差异配对":
					o.TargetPass = i >= 20
					o.Target.InputTokens = int64Pointer(50)
					o.Target.ResponseModel = "claude-haiku-4-5"
					o.Target.TTFT, o.Target.Duration = 3*time.Second, 8*time.Second
					o.Target.ReasoningTokens = 20
					if key == "A11.thinking" {
						o.Target.ReasoningTokens = 0
					}
					if key == "A04" || key == "A05" {
						o.Target.HTTPStatus, o.Target.ErrorClass = 403, "http_4xx"
						o.Target.ErrorCode = "model_access_denied"
						if key == "A05" {
							o.Target.ErrorCode = "context_length_exceeded"
						}
					}
					if key == "A15" {
						o.Target.ErrorClass, o.Target.ErrorCode, o.Target.HTTPStatus = "expected_reject", "不同参数规则", 400
					}
					if key == "D07" && i < 3 {
						o.Target.Flags = []string{"refusal"}
					}
				case "基线未兑现":
					o.RefPass = false
					o.Ref.ReasoningTokens = 0
					if key == "A04" || key == "A05" {
						o.Ref.HTTPStatus, o.Ref.ErrorClass = 503, "http_5xx"
					}
				case "部分证据":
					if i%3 == 0 {
						o.Ref = nil
					}
					if i%5 == 0 {
						o.Target.Flags = []string{"truncated"}
					}
					if i%7 == 0 {
						o.Target.ReasoningObserved = false
					}
				}
				rows = append(rows, o)
			}
		}
		for i := 0; i < 10; i++ {
			o := pairedInspectionOutcome("D06", true, true)
			o.ItemID, o.InstanceIdx, o.GeneratorVersion = int64(i), 1000, "math/1"
			rows = append(rows, o)
		}
		if name == "未校准" {
			st = defaultInspectionSettings()
		}
		if name == "禁用功能" {
			for _, key := range []string{"D07", "A11.tools", "A05"} {
				o := st.CheckOverrides[key]
				o.Enabled = capabilityBoolPointer(false)
				st.CheckOverrides[key] = o
			}
		}
		cases = append(cases, pairedRegressionCase{name, rows, st})
	}
	return cases
}

func TestPairedInspectionPreservesExistingEvidenceRules(t *testing.T) {
	// 旧快照保留样本剔除与功能结论；新的模型期望及流程结论单独验证。
	raw, err := os.ReadFile("testdata/inspection_paired_5834d16.json")
	if err != nil {
		t.Fatal(err)
	}
	var expected map[string]pairedRegressionResult
	if err := json.Unmarshal(raw, &expected); err != nil {
		t.Fatal(err)
	}
	for _, tc := range pairedRegressionCases() {
		t.Run(tc.name, func(t *testing.T) {
			checks, gates := Inspect(tc.rows, tc.st, time.Time{}, InspectOptions{Capability: &inspection.ModelCapability{ContextWindow: 1, Tools: capabilityBoolPointer(false)}})
			for key, old := range expected[tc.name].Checks {
				current := checks[key]
				if current.Status != old.Status || current.Samples != old.Samples || current.Evidence.Source != old.Evidence.Source {
					t.Fatalf("%s/%s 的样本与功能判定改变：%+v，旧=%+v", tc.name, key, current, old)
				}
			}
			if len(gates) != len(expected[tc.name].Gates) {
				t.Fatal("已有权限和可用门槛证据改变")
			}
			if a := assessFormal(tc.rows, th, tc.st); a.Samples != 0 || a.LLR != 0 {
				t.Fatal("配对总结不应继续输出旧SPRT结论")
			}

		})
	}
	if len(expected) != len(pairedRegressionCases()) || !strings.Contains(string(raw), "paired_probe") {
		t.Fatal("配对快照夹具不完整")
	}
}
