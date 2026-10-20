package inspection

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

func number(value float64) *float64 { return &value }

func calibrated(keys ...string) Settings {
	s := Defaults()
	s.Mode = ModeFormal
	for _, key := range keys {
		pricing := true
		s.CheckOverrides[key] = CheckOverride{Pricing: &pricing, Calibrated: true, CalibrationNote: "型号：示例型号；入口：同类入口；网关版本：测试版本；样本：100；误报：0；记录标识：测试记录"}
	}
	return s
}

func sample(key string, factor float64) Check {
	return Check{Key: key, Status: "warning", Factor: number(factor), Samples: Samples{Observed: 20, Paired: 20}, Metrics: map[string]Metric{}, Evidence: Evidence{Source: "paired_probe", SampleUnit: "item_unit"}}
}

func TestAggregateGroupsAndFloors(t *testing.T) {
	checks := map[string]Check{
		"D06": sample("D06", 0.85), "D07": sample("D07", 0.7),
		"A11.thinking": sample("A11.thinking", 0.95), "A11.tools": sample("A11.tools", 0.95),
		"A11.json_schema": sample("A11.json_schema", 0.9), "A11.multiturn": sample("A11.multiturn", 0.8),
		"A11.image_pdf": sample("A11.image_pdf", 0.95), "D12": sample("D12", 0.85),
		"D13": sample("D13", 0.7), "D04": sample("D04", 0.85), "D10": sample("D10", 1.05),
	}
	var keys []string
	for key := range checks {
		keys = append(keys, key)
	}
	result := Aggregate(checks, nil, ModeFormal, calibrated(keys...))
	want := map[string]float64{"identity": 1, "capability": 0.7, "function": 0.7, "availability": 0.6, "performance": 1.05}
	if !reflect.DeepEqual(result.GroupFactors, want) || result.QualityFactor == nil || *result.QualityFactor != 0.5 {
		t.Fatalf("组系数或总下限错误：%+v", result)
	}
	if !result.Checks["D06"].Applied || checks["D06"].Applied {
		t.Fatal("正式标记缺失或修改了输入")
	}
}

func TestAggregateFunctionFacesMerge(t *testing.T) {
	checks := map[string]Check{"A11.tools": sample("A11.tools", 0.95), "A11.json_schema": sample("A11.json_schema", 0.9), "A11.multiturn": sample("A11.multiturn", 0.85)}
	result := Aggregate(checks, nil, ModeFormal, calibrated("A11.tools", "A11.json_schema", "A11.multiturn"))
	if result.QualityFactor == nil || math.Abs(*result.QualityFactor-0.85) > 1e-12 {
		t.Fatalf("同根因的工具、结构化、多轮不应相乘：%+v", result)
	}
}

func TestAggregatePricingDisabledAndEmptyEvidence(t *testing.T) {
	for _, test := range []struct {
		name     string
		check    Check
		settings Settings
	}{
		{"默认关闭扣价", sample("D06", 0.7), Defaults()},
		{"启用不等于扣价", sample("D06", 0.7), func() Settings {
			s := Defaults()
			enabled := true
			s.CheckOverrides["D06"] = CheckOverride{Enabled: &enabled}
			return s
		}()},
		{"已校准不等于扣价", sample("D06", 0.7), func() Settings {
			s := Defaults()
			s.CheckOverrides["D06"] = CheckOverride{Calibrated: true, CalibrationNote: "登记记录"}
			return s
		}()},
		{"结论不确定", func() Check { c := sample("D06", 0.7); c.Status = "inconclusive"; return c }(), calibrated("D06")},
		{"未检测", func() Check { c := sample("D06", 0.7); c.Status = "not_run"; return c }(), calibrated("D06")},
		{"没有样本", func() Check { c := sample("D06", 0.7); c.Samples = Samples{}; return c }(), calibrated("D06")},
		{"检测项关闭", sample("D06", 0.7), func() Settings {
			s := calibrated("D06")
			enabled := false
			override := s.CheckOverrides["D06"]
			override.Enabled = &enabled
			s.CheckOverrides["D06"] = override
			return s
		}()},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := Aggregate(map[string]Check{test.check.Key: test.check}, nil, ModeFormal, test.settings)
			if result.QualityFactor != nil || result.Checks[test.check.Key].Applied {
				t.Fatalf("无有效定价观测不能估价：%+v", result)
			}
		})
	}
	if result := Aggregate(nil, nil, ModeFormal, Defaults()); result.QualityFactor != nil {
		t.Fatal("空批次产生 Q")
	}
}

func TestAggregateTrialAndGates(t *testing.T) {
	checks := map[string]Check{"D06": sample("D06", 0.85), "A04": sample("A04", 1)}
	s := calibrated("D06", "A04")
	trial := Aggregate(checks, nil, ModeTrial, s)
	if trial.QualityFactor == nil || *trial.QualityFactor != 0.85 || !trial.Checks["D06"].Applied || trial.Provisional {
		t.Fatalf("试运行扣价未生效：%+v", trial)
	}
	gate := Gate{CheckKey: "A04", Health: "unavailable", Confirmed: true}
	for _, mode := range []string{ModeTrial, ModeFormal} {
		result := Aggregate(checks, []Gate{gate}, mode, s)
		if result.Gates[0].Confirmed != (mode == ModeFormal) || result.Checks["A04"].Applied != (mode == ModeTrial) || result.Checks["D06"].Applied != (mode == ModeTrial) || (result.QualityFactor == nil) != (mode == ModeFormal) {
			t.Fatalf("模式 %s 的门槛处理错误：%+v", mode, result)
		}
	}
	s.CheckOverrides["A04"] = CheckOverride{}
	result := Aggregate(checks, []Gate{gate}, ModeFormal, s)
	if result.QualityFactor == nil || result.Gates[0].Confirmed || !result.Checks["D06"].Applied {
		t.Fatal("未校准的门槛影响了正式结果")
	}
}

func TestAggregatePricingIndependentOfModeRoleAndCalibration(t *testing.T) {
	for _, mode := range []string{ModeTrial, ModeFormal} {
		for _, key := range []string{"D07", "A02", "A04", "third_party"} {
			for _, status := range []string{"pass", "warning", "fail"} {
				t.Run(mode+"/"+key+"/"+status, func(t *testing.T) {
					s := Defaults()
					pricing := true
					// 不登记校准，确认展示项和门槛项也能独立开启扣价。
					s.CheckOverrides[key] = CheckOverride{Pricing: &pricing}
					check := sample(key, 1)
					check.Status, check.Factor = status, nil
					result := Aggregate(map[string]Check{key: check}, nil, mode, s)
					definition, _ := Effective(key, s)
					want := 1.0
					switch status {
					case "warning":
						want = definition.Coefficients["warning"]
					case "fail":
						want = definition.Coefficients["severe"]
					}
					if result.QualityFactor == nil || math.Abs(*result.QualityFactor-want) > 1e-12 || !result.Checks[key].Applied || result.Checks[key].Factor == nil || *result.Checks[key].Factor != want || result.Provisional {
						t.Fatalf("模式、角色或校准限制了扣价，期望 %v：%+v", want, result)
					}
				})
			}
		}
	}
}

func TestAggregateIdentityMinimumAndFloor(t *testing.T) {
	checks := map[string]Check{"A02": sample("A02", 0.85), "A03": sample("A03", 0.7), "third_party": sample("third_party", 0.75)}
	s := calibrated("A02", "A03", "third_party")
	result := Aggregate(checks, nil, ModeTrial, s)
	if result.QualityFactor == nil || *result.QualityFactor != 0.7 || result.GroupFactors["identity"] != 0.7 {
		t.Fatalf("同组身份项重复相乘：%+v", result)
	}
	checks["A03"] = sample("A03", 0.3)
	result = Aggregate(checks, nil, ModeTrial, s)
	if result.QualityFactor == nil || *result.QualityFactor != 0.6 || result.GroupFactors["identity"] != 0.6 {
		t.Fatalf("身份组未遵守下限：%+v", result)
	}
}

func TestAggregateDisplayChecksUseConfiguredCoefficients(t *testing.T) {
	for _, key := range []string{"A01", "A06", "A08", "D05"} {
		t.Run(key, func(t *testing.T) {
			s := Defaults()
			pricing := true
			s.CheckOverrides[key] = CheckOverride{Pricing: &pricing, Coefficients: map[string]float64{"warning": 0.85}}
			check := sample(key, 1)
			check.Factor = nil
			result := Aggregate(map[string]Check{key: check}, nil, ModeTrial, s)
			if result.QualityFactor == nil || *result.QualityFactor != 0.85 || !result.Checks[key].Applied {
				t.Fatalf("展示项自定义扣价未计入 Q：%+v", result)
			}
		})
	}
}

func TestAggregateFunctionPermissionBoundaryAndOutputRemainIndependent(t *testing.T) {
	checks := map[string]Check{"A04": sample("A04", 0.9), "A05": sample("A05", 0.9), "D08": sample("D08", 0.95)}
	result := Aggregate(checks, nil, ModeTrial, calibrated("A04", "A05", "D08"))
	if result.QualityFactor == nil || math.Abs(*result.QualityFactor-0.9*0.9*0.95) > 1e-12 || result.GroupFactors["capability"] != 1 || math.Abs(result.GroupFactors["function"]-0.9*0.9*0.95) > 1e-12 {
		t.Fatalf("权限、上下文边界与输出应在功能组各自成桶：%+v", result)
	}
}

func TestAggregateThirdPartyGateDoesNotRequireCalibration(t *testing.T) {
	s := Defaults()
	pricing := true
	s.CheckOverrides["third_party"] = CheckOverride{Pricing: &pricing}
	checks := map[string]Check{"third_party": sample("third_party", 0.7)}
	gate := Gate{CheckKey: "third_party", Health: "degraded", Confirmed: true}
	trial := Aggregate(checks, []Gate{gate}, ModeTrial, s)
	if trial.Gates[0].Confirmed || trial.QualityFactor == nil || !trial.Checks["third_party"].Applied {
		t.Fatalf("试运行第三方门槛阻止扣价：%+v", trial)
	}
	formal := Aggregate(checks, []Gate{gate}, ModeFormal, s)
	if !formal.Gates[0].Confirmed || formal.QualityFactor != nil || formal.Checks["third_party"].Applied {
		t.Fatalf("正式第三方门槛应优先暂停且保留既有 Q：%+v", formal)
	}
}

func TestAggregatePerformanceBonus(t *testing.T) {
	result := Aggregate(map[string]Check{"D10": sample("D10", 1.05)}, nil, ModeFormal, calibrated("D10"))
	if result.QualityFactor == nil || *result.QualityFactor != 1.05 {
		t.Fatal("性能奖励被中性 1 吃掉")
	}
}

func TestAggregateInvalidFactorIsUnknown(t *testing.T) {
	for _, value := range []float64{0, -1, 1.1, math.NaN(), math.Inf(1)} {
		result := Aggregate(map[string]Check{"D06": sample("D06", value)}, nil, ModeFormal, calibrated("D06"))
		if result.QualityFactor != nil || result.Checks["D06"].Factor != nil {
			t.Fatalf("无效系数未归为未知：%+v", result)
		}
		if _, err := json.Marshal(result); err != nil {
			t.Fatal("无效候选系数导致报告不能保存：", err)
		}
	}
}

func TestGradeBoundariesAndFirstListing(t *testing.T) {
	s := calibrated()
	history := GradeHistory{GateFreeDays: 30, AvailabilityDays: 7, Availability: 0.995}
	for _, test := range []struct {
		q        float64
		expected string
	}{{0.5, "D"}, {0.65, "C"}, {0.82, "B"}, {0.93, "A"}, {1.02, "S"}} {
		if result := Grade(number(test.q), "", "", 0, history, s); result.Grade != test.expected {
			t.Fatalf("Q=%v：%+v", test.q, result)
		}
	}
	history.FirstListing = true
	if result := Grade(number(1.05), "", "", 0, history, s); result.Grade != "A" {
		t.Fatalf("首次上架不能授予 S：%+v", result)
	}
	if result := Grade(number(1.05), "", "", 0, GradeHistory{}, s); result.Grade != "A" {
		t.Fatal("缺历史不能授予 S")
	}
}

func TestGradeHysteresisAndContinuousDowngrade(t *testing.T) {
	s := calibrated()
	if result := Grade(number(0.91), "A", "B", 1, GradeHistory{}, s); result.Grade != "A" || result.PendingGradeCount != 0 {
		t.Fatalf("降级边界等值应保持且打断连续计数：%+v", result)
	}
	first := Grade(number(0.909), "A", "", 0, GradeHistory{}, s)
	if first.Grade != "A" || first.PendingGrade != "B" || first.PendingGradeCount != 1 {
		t.Fatalf("首次降级应待确认：%+v", first)
	}
	second := Grade(number(0.909), first.Grade, first.PendingGrade, first.PendingGradeCount, GradeHistory{}, s)
	if second.Grade != "B" || second.PendingGrade != "" || second.PendingGradeCount != 0 {
		t.Fatalf("连续两次未确认降级：%+v", second)
	}
	if result := Grade(nil, "A", "B", 1, GradeHistory{}, s); result.Grade != "A" || result.PendingGradeCount != 0 {
		t.Fatal("不确定结果应打断连续计数")
	}
	if result := Grade(number(0.94), "B", "", 0, GradeHistory{}, s); result.Grade != "B" {
		t.Fatal("升级未达到 +0.02")
	}
	if result := Grade(number(0.95), "B", "", 0, GradeHistory{}, s); result.Grade != "A" {
		t.Fatal("升级等于 +0.02 应通过")
	}
	if result := Grade(number(0.7), "A", "B", 1, GradeHistory{}, s); result.PendingGrade != "C" || result.PendingGradeCount != 1 {
		t.Fatal("候选等级改变不能继承连续次数")
	}
}

func TestGradeSHistoryAndTrial(t *testing.T) {
	s := calibrated()
	history := GradeHistory{GateFreeDays: 30, AvailabilityDays: 7, Availability: 0.995}
	if result := Grade(number(1.039), "A", "", 0, history, s); result.Grade != "A" {
		t.Fatal("S 升级未达到 1.04")
	}
	if result := Grade(number(1.04), "A", "", 0, history, s); result.Grade != "S" {
		t.Fatal("有效历史下未升级 S")
	}
	history.Availability = 0.994
	first := Grade(number(1.05), "S", "", 0, history, s)
	second := Grade(number(1.05), first.Grade, first.PendingGrade, first.PendingGradeCount, history, s)
	if first.Grade != "S" || first.PendingGrade != "A" || second.Grade != "A" {
		t.Fatalf("S 资格失效也须连续确认：%+v %+v", first, second)
	}
	s.Mode = ModeTrial
	if result := Grade(number(0.5), "S", "B", 1, GradeHistory{}, s); result != (GradeDecision{Grade: "S", PendingGrade: "B", PendingGradeCount: 1}) {
		t.Fatal("试运行改变了正式等级或状态")
	}
}

func TestAppraiseContractAndFreshBase(t *testing.T) {
	at := time.Date(2026, 10, 12, 4, 0, 0, 0, time.UTC)
	base := map[string]float64{"max_20x": 1.5, "default": 1.2}
	result := Appraise(base, "max_20x", number(0.85), &at, true, false)
	if !result.Configured || !result.Provisional || result.Factor == nil || math.Abs(*result.Multiplier-1.275) > 1e-12 || result.CheckedAt != &at {
		t.Fatalf("估价错误：%+v", result)
	}
	base["max_20x"] = 2
	if changed := Appraise(base, "max_20x", number(0.85), &at, false, false); math.Abs(*changed.Multiplier-1.7) > 1e-12 || changed.CheckedAt != &at {
		t.Fatal("新基准未即时生效或伪造检测时间")
	}
	if result := Appraise(base, "unknown", number(1), &at, false, false); *result.Base != 1.2 {
		t.Fatal("缺档位未使用 default")
	}
	for _, missing := range []Appraisal{Appraise(nil, "max", number(1), &at, false, false), Appraise(base, "max", nil, &at, false, false), Appraise(base, "max", number(1), nil, false, false)} {
		if missing.Multiplier != nil || missing.Factor != nil {
			t.Fatal("缺配置、Q 或检测时间不能估价")
		}
	}
}

func TestPublicChecksRedaction(t *testing.T) {
	check := sample("D06", 0.7)
	check.Reason, check.Advice, check.Impact = "秘密题面和阈值", "基线身份", "精确阈值"
	version := "内部版本"
	check.Evidence.GeneratorVersion, check.Evidence.Code = &version, "内部定位"
	check.Metrics["secret"] = Metric{Value: number(0.6), Unit: "ratio"}
	result := PublicChecks(map[string]Check{"D06": check, "unknown": check})
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"秘密", "阈值", "基线身份", "内部", "secret"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("卖家结果包含 %s：%s", secret, raw)
		}
	}
	if len(result) != 1 || len(result["D06"].Metrics) != 0 || len(check.Metrics) != 1 || result["D06"].Evidence.Code != "" {
		t.Fatal("白名单脱敏或输入隔离失败")
	}
}

func TestPublicChecksOnlyKeepAppliedFactors(t *testing.T) {
	for _, applied := range []bool{false, true} {
		check := sample("A02", 0.7)
		check.Applied = applied
		check.Impact = "试运行旧说明"
		public := PublicChecks(map[string]Check{"A02": check})["A02"]
		if (public.Factor != nil) != applied || public.Applied != applied || applied && *public.Factor != 0.7 {
			t.Fatalf("扣价系数公开范围错误：%+v", public)
		}
		if strings.Contains(public.Impact, "试运行") || check.Factor == nil {
			t.Fatal("公开说明沿用旧模式规则或修改了输入")
		}
	}
}

func TestPublicItemChecksRedactionAndAdvice(t *testing.T) {
	for _, tt := range []struct{ code, status, reason string }{
		{"item_pass", "pass", "本次抽样观测通过。"},
		{"item_pass_rate_low", "warning", "本次抽样观测发现需要关注的差异。"},
		{"item_function_failed", "fail", "本次抽样观测未通过，建议修复后复检。"},
		{"capability_unsupported", "not_applicable", "本次检测条件不适用。"},
		{"capability_unknown", "inconclusive", "模型官方能力尚未登记，暂时无法判定此项。"},
		{"needs_baseline", "inconclusive", "该项需要基线账号对照，暂时无法判定。"},
	} {
		t.Run(tt.code, func(t *testing.T) {
			c := sample("D07", .7)
			c.Applied = true
			c.Status, c.Reason = tt.status, "私有题面、判分器和期望阈值"
			c.Evidence.Source, c.Evidence.Code = "item_probe", tt.code
			c.Metrics["expected_pass_rate"] = Metric{Value: number(.9), Unit: "fraction"}
			public := PublicChecks(map[string]Check{"D07": c})["D07"]
			if public.Evidence.Source != "item_probe" || public.Evidence.Code != "" || len(public.Metrics) != 0 || public.Factor == nil || public.Reason != tt.reason {
				t.Fatalf("卖家摘要未脱敏或丢失安全说明：%+v", public)
			}
			raw, _ := json.Marshal(public)
			for _, secret := range []string{tt.code, "expected_pass_rate", "私有题面", "判分器", "0.9"} {
				if strings.Contains(string(raw), secret) {
					t.Fatalf("卖家摘要泄露 %s：%s", secret, raw)
				}
			}
			if c.Factor == nil || len(c.Metrics) != 1 {
				t.Fatal("脱敏改写内部证据")
			}
		})
	}
}
