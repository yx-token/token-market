package quality

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRecoveryAccountExcludesOtherRequests(t *testing.T) {
	e := Engine{}
	u := &upstreamRow{id: 1}
	p := &Policy{ItemParallelism: 4}
	st := defaultInspectionSettings()
	release, err := e.acquireRecovery(context.Background(), u)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := e.acquireRequest(ctx, u, p, st, false); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("恢复采样期间普通请求未被阻止：%v", err)
	}
	requestRelease, err := e.acquireRequest(context.Background(), u, p, st, true)
	if err != nil {
		t.Fatal(err)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel2()
	if _, err := e.acquireRequest(ctx2, u, p, st, true); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("恢复样本超过账号单并发：%v", err)
	}
	requestRelease()
	release()
	ordinary, err := e.acquireRequest(context.Background(), u, p, st, false)
	if err != nil {
		t.Fatal(err)
	}
	ordinary()
}

func TestInspectionWaitSupportsCancellationAndInjection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e := Engine{}
	if err := e.wait(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消未终止等待：%v", err)
	}
	var waited time.Duration
	e.Wait = func(_ context.Context, delay time.Duration) error { waited += delay; return nil }
	if err := e.wait(context.Background(), 3*time.Second); err != nil || waited != 3*time.Second {
		t.Fatalf("等待未通过测试时钟：%v %v", waited, err)
	}
}

func TestRecoverySampleRequestsAreDistinctAndTargetOnly(t *testing.T) {
	e := Engine{Keyring: testInspectionKeyring(t)}
	plan := executionSnapshot{Instances: []plannedInstance{{CheckKey: "A04", Steps: []plannedStep{{Request: ProbeRequest{Prompt: "只输出 READY。"}}}}}}
	if err := e.addAvailabilityRecoveryProbes(&plan, "恢复样本去重", 20); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, it := range plan.Instances[1:] {
		prompt := it.Steps[0].Request.Prompt
		if !it.TargetOnly || seen[prompt] {
			t.Fatalf("额外恢复请求必须仅发被测侧且输入不同：%+v", it)
		}
		seen[prompt] = true
	}
	if plan.Instances[0].TargetOnly || len(seen) != 20 {
		t.Fatal("额外恢复采样不能改动普通配对计划或丢失样本")
	}
}
