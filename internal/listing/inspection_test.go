package listing

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/DueGin/token-market/internal/inspection"
)

func TestAppraisalBaseIsIndependentOfBuyback(t *testing.T) {
	v := VendorConfig{BuybackBase: map[string]float64{"default": 0.2}}
	if _, configured := v.AppraisalBaseFor("pro"); configured {
		t.Fatal("缺少质检估价基准时不能回退到回收基准")
	}
	v.AppraisalBase = map[string]float64{"default": 0.3, "pro": 0.4}
	for _, row := range []struct {
		tier string
		want float64
	}{{"pro", 0.4}, {"unknown", 0.3}, {"", 0.3}} {
		got, configured := v.AppraisalBaseFor(row.tier)
		if !configured || got != row.want {
			t.Fatalf("档位 %s：%v %t", row.tier, got, configured)
		}
	}
	for _, base := range []map[string]float64{nil, {}, {"pro": 0.4}, {"default": 0.3}} {
		if err := validateAppraisalBase(base); err != nil {
			t.Fatalf("有效估价基准被拒绝：%v", err)
		}
	}
	for _, base := range []map[string]float64{{"": 0.3}, {" ": 0.3}, {"pro": 0}, {"pro": -0.1}, {"pro": math.NaN()}, {"pro": math.Inf(1)}} {
		if err := validateAppraisalBase(base); err == nil {
			t.Fatalf("无效估价基准未拒绝：%v", base)
		}
	}
}

func TestInspectionAppraisalUsesCurrentBaseAndValidEvidence(t *testing.T) {
	at := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)
	q := 0.8
	in := inspectionViewInput{EvidenceMode: inspection.ModeFormal, Tier: "pro", Base: map[string]float64{"pro": 0.2}, Factor: &q, AppraisedAt: &at, LastCheckedAt: &at}
	trial := makeInspectionView(inspection.ModeTrial, in)
	if trial.Appraisal.Multiplier == nil || math.Abs(*trial.Appraisal.Multiplier-0.16) > 1e-9 || !trial.Appraisal.Provisional || !trial.Appraisal.Configured {
		t.Fatalf("试运行质检估价错误：%+v", trial)
	}
	in.Base["pro"] = 0.4
	formal := makeInspectionView(inspection.ModeFormal, in)
	if formal.Appraisal.Multiplier == nil || math.Abs(*formal.Appraisal.Multiplier-0.32) > 1e-9 || formal.Appraisal.Provisional || formal.Appraisal.CheckedAt == nil || !formal.Appraisal.CheckedAt.Equal(at) {
		t.Fatalf("基准调整须立即重算，不能伪造检测时间：%+v", formal)
	}
	for _, row := range []struct {
		name string
		mut  func(*inspectionViewInput)
	}{{"未检测", func(in *inspectionViewInput) { in.AppraisedAt = nil }}, {"无有效Q", func(in *inspectionViewInput) { in.Factor = nil }}, {"已确认门槛", func(in *inspectionViewInput) { in.ConfirmedGate = true }}, {"门槛首次复测尚未恢复", func(in *inspectionViewInput) { in.Health = HealthUnavailable }}, {"降智尚未恢复", func(in *inspectionViewInput) { in.Health = HealthDegraded }}} {
		t.Run(row.name, func(t *testing.T) {
			data := in
			row.mut(&data)
			view := makeInspectionView(inspection.ModeFormal, data)
			if view.QualityFactor != nil || view.Appraisal.Factor != nil || view.Appraisal.Multiplier != nil {
				t.Fatalf("无有效估价证据时必须暂不估价：%+v", view)
			}
		})
	}
	in.Base = nil
	missing := makeInspectionView(inspection.ModeFormal, in)
	if missing.Appraisal.Configured || missing.Appraisal.Base != nil || missing.Appraisal.Multiplier != nil {
		t.Fatalf("缺少基准须显示未配置：%+v", missing)
	}
}

func TestInspectionModeSwitchDoesNotPromoteTrialEvidence(t *testing.T) {
	q := 0.8
	at := time.Now()
	in := inspectionViewInput{Base: map[string]float64{"default": 0.3}, Factor: &q, AppraisedAt: &at, LastCheckedAt: &at}
	for _, row := range []struct {
		name        string
		currentMode string
		runMode     string
		wantMode    string
		provisional bool
	}{{"试运行记录切入正式", inspection.ModeFormal, inspection.ModeTrial, inspection.ModeTrial, true},
		{"旧记录没有模式", inspection.ModeFormal, "", inspection.ModeTrial, true},
		{"正式记录切回试运行", inspection.ModeTrial, inspection.ModeFormal, inspection.ModeFormal, true},
		{"正式模式与正式记录", inspection.ModeFormal, inspection.ModeFormal, inspection.ModeFormal, false}} {
		t.Run(row.name, func(t *testing.T) {
			data := in
			data.EvidenceMode = row.runMode
			view := makeInspectionView(row.currentMode, data)
			if view.InspectionMode != row.wantMode || view.Appraisal.Provisional != row.provisional {
				t.Fatalf("模式切换不能改变历史证据的正式性：%+v", view)
			}
			if view.Appraisal.Multiplier == nil || math.Abs(*view.Appraisal.Multiplier-0.24) > 1e-9 {
				t.Fatalf("试运行仍须展示明确标记的估价预览：%+v", view)
			}
		})
	}
}

func TestInspectionViewRemovesInternalEvidence(t *testing.T) {
	q := 0.8
	at := time.Now()
	in := inspectionViewInput{Factor: &q, AppraisedAt: &at, Checks: map[string]inspection.Check{
		"D07": {Key: "D07", Name: "智力配对", Category: "capability", Status: "fail", Role: "factor", Factor: &q,
			Reason: "基线账号 secret-account 从 192.0.2.10 返回答案 secret-answer；阈值 0.87654321", Advice: "查看题面 secret-prompt",
			Metrics: map[string]inspection.Metric{"internal_threshold": {Value: &q}}, Evidence: inspection.Evidence{Code: "secret-code", Source: "paired_probe"}},
	}}
	view := makeInspectionView(inspection.ModeTrial, in)
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"secret-account", "secret-answer", "secret-prompt", "secret-code", "192.0.2.10", "0.87654321", "internal_threshold"} {
		if strings.Contains(string(raw), private) {
			t.Fatalf("公共质检摘要泄露内部证据 %s：%s", private, raw)
		}
	}
	if view.QualityChecks["D07"].Key != "D07" || view.QualityChecks["D07"].Status != "fail" {
		t.Fatal("脱敏不能丢掉质检项状态")
	}
}
