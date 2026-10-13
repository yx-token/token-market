package quality

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/DueGin/token-market/internal/inspection"
)

type cacheStepLoader struct {
	steps map[int64]*[2]int64
	calls []int64
	err   error
}

func (l *cacheStepLoader) load(_ context.Context, id int64, it plannedInstance) (*[2]int64, error) {
	l.calls = append(l.calls, id)
	if len(it.Steps) != 2 {
		return nil, errors.New("缓存夹具应读取两步实例")
	}
	return l.steps[id], l.err
}

func TestItemCacheUsesOnlyTargetStepLoader(t *testing.T) {
	st := calibratedInspectionSettings()
	d, _ := inspection.Effective("D12", st)
	it := plannedInstance{ItemID: 1, CheckKey: "D12", Steps: make([]plannedStep, 2)}
	plan := executionSnapshot{Instances: []plannedInstance{it}, Policy: &Policy{Platform: "anthropic"}}
	for _, tt := range []struct {
		name    string
		steps   *[2]int64
		support *bool
		status  string
	}{
		{"命中", &[2]int64{0, 40}, nil, "pass"}, {"支持未命中", &[2]int64{}, capabilityBoolPointer(true), "warning"},
		{"不支持", &[2]int64{}, capabilityBoolPointer(false), "not_applicable"}, {"未登记", &[2]int64{}, nil, "inconclusive"},
		{"用量缺失", nil, capabilityBoolPointer(true), "inconclusive"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			loader := &cacheStepLoader{steps: map[int64]*[2]int64{1: tt.steps}}
			checks := map[string]inspection.Check{"D12": newInspectionCheck(d, time.Time{})}
			err := inspectCacheChecks(context.Background(), runRow{id: 1, model: "claude-opus-5-5"}, nil, plan, st, checks, loader.load, inspection.ModelCapability{PromptCache: tt.support})
			if c := checks["D12"]; err != nil || c.Status != tt.status || c.Evidence.Source != "item_probe" || !reflect.DeepEqual(loader.calls, []int64{1}) {
				t.Fatalf("两步读取或能力判定错误：%+v %+v %v", c, loader.calls, err)
			}
		})
	}
	loader := &cacheStepLoader{steps: map[int64]*[2]int64{1: &[2]int64{}}}
	checks := map[string]inspection.Check{"D12": newInspectionCheck(d, time.Time{})}
	if err := inspectCacheChecks(context.Background(), runRow{id: 1, model: "claude-opus-5-5"}, nil, plan, st, checks, loader.load); err != nil || checks["D12"].Status != "warning" {
		t.Fatal("执行快照的平台能力未生效", err)
	}
	loader.err = errors.New("缓存读取失败")
	if err := inspectCacheChecks(context.Background(), runRow{id: 1}, nil, plan, st, checks, loader.load); !errors.Is(err, loader.err) {
		t.Fatal("读取错误未传递")
	}
}

func TestPairedCacheKeepsComparison(t *testing.T) {
	st := calibratedInspectionSettings()
	d, _ := inspection.Effective("D12", st)
	plan := executionSnapshot{Instances: []plannedInstance{{ItemID: 1, CheckKey: "D12", Steps: make([]plannedStep, 2)}}}
	for _, tt := range []struct {
		name              string
		target, reference int64
		status            string
	}{
		{"两侧命中", 1, 1, "pass"}, {"基线命中被测未命中", 0, 1, "fail"}, {"基线未命中", 1, 0, "not_applicable"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			loader := &cacheStepLoader{steps: map[int64]*[2]int64{1: &[2]int64{0, tt.target}, 2: &[2]int64{0, tt.reference}}}
			checks := map[string]inspection.Check{"D12": newInspectionCheck(d, time.Time{})}
			err := inspectCacheChecks(context.Background(), runRow{id: 1}, &runRow{id: 2}, plan, st, checks, loader.load, inspection.ModelCapability{PromptCache: capabilityBoolPointer(false)})
			if c := checks["D12"]; err != nil || c.Status != tt.status || c.Evidence.Source != "paired_probe" || !reflect.DeepEqual(loader.calls, []int64{1, 2}) {
				t.Fatalf("配对缓存被能力登记改变：%+v %+v %v", c, loader.calls, err)
			}
		})
	}
}
