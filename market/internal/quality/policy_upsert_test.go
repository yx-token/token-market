package quality

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
)

var errPolicyTestStop = errors.New("stop before returning saved policy")

type policyTestRow struct {
	values []any
	err    error
}

func (r policyTestRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return fmt.Errorf("scan got %d destinations, want %d", len(dest), len(r.values))
	}
	for i, value := range r.values {
		target := reflect.ValueOf(dest[i])
		if target.Kind() != reflect.Pointer || target.IsNil() {
			return fmt.Errorf("scan destination %d is not a pointer", i)
		}
		if value == nil {
			target.Elem().SetZero()
			continue
		}
		source := reflect.ValueOf(value)
		if !source.Type().AssignableTo(target.Elem().Type()) {
			return fmt.Errorf("scan value %d has type %T", i, value)
		}
		target.Elem().Set(source)
	}
	return nil
}

type policyTestDB struct {
	policy   []any
	inserted []any
}

func (db *policyTestDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("unexpected Exec")
}

func (db *policyTestDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected Query")
}

func (db *policyTestDB) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	switch {
	case strings.Contains(query, "FROM vendor_configs"):
		return policyTestRow{values: []any{"openai", true, []string{"apikey"}, 0.0, 10.0, []byte(`{}`), []byte(`{}`), 1, []byte(`{}`), time.Unix(0, 0)}}
	case strings.Contains(query, "FROM detection_policies WHERE"):
		return policyTestRow{values: db.policy}
	case strings.Contains(query, "SELECT EXISTS(SELECT 1 FROM detection_providers"):
		return policyTestRow{values: []any{false}}
	case strings.Contains(query, "INSERT INTO detection_policies"):
		db.inserted = append([]any(nil), args...)
		return policyTestRow{err: errPolicyTestStop}
	default:
		return policyTestRow{err: fmt.Errorf("unexpected query: %s", query)}
	}
}

func newPolicyTestDB(flows []byte) *policyTestDB {
	return &policyTestDB{policy: []any{
		int64(1), "openai", "", true, false, 360, 30, []string{"availability"}, []string{"旧模型"}, 1.0,
		[]byte(`{"h0":0.9,"h1":0.7,"alpha":0.05,"beta":0.1}`), true, "B", []byte(`[]`), int64(1), time.Unix(0, 0),
		4, []string{"admin"}, []byte(`{}`), flows,
	}}
}

func policyTestPtr[T any](value T) *T { return &value }

func policyTestInput(t *testing.T, input PolicyInput) PolicyInput {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var decoded PolicyInput
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func (db *policyTestDB) savedFlows(t *testing.T) PolicyFlows {
	t.Helper()
	if len(db.inserted) != 18 {
		t.Fatalf("策略没有进入预期写入：%v", db.inserted)
	}
	var flows PolicyFlows
	if err := json.Unmarshal(db.inserted[17].([]byte), &flows); err != nil {
		t.Fatal(err)
	}
	return flows
}

func TestUpsertPolicyLegacyTopLevelPatchUsesNewValues(t *testing.T) {
	db := newPolicyTestDB([]byte(`{}`))
	input := policyTestInput(t, PolicyInput{
		Models:           policyTestPtr([]string{"新模型"}),
		FullTriggers:     policyTestPtr([]string{"admin", "appeal"}),
		ScheduledEnabled: policyTestPtr(true),
	})
	_, err := UpsertPolicy(context.Background(), db, "openai", "", input, 1)
	if !errors.Is(err, errPolicyTestStop) {
		t.Fatalf("保存未走到写入：%v", err)
	}
	flows := db.savedFlows(t)
	if !flows.Intelligence.ScheduledEnabled || !reflect.DeepEqual(flows.Intelligence.FullTriggers, []string{"admin", "appeal"}) ||
		len(flows.Intelligence.Models) != 1 || flows.Intelligence.Models[0].Model != "新模型" {
		t.Fatalf("顶层补丁没有进入实际 flows：%+v", flows.Intelligence)
	}
}

func TestUpsertPolicyTopLevelPatchPreservesConfiguredFlows(t *testing.T) {
	want := policyFlowsFixture()
	want.Identity.Source = "self"
	db := newPolicyTestDB(mustJSON(want))
	input := policyTestInput(t, PolicyInput{Enabled: policyTestPtr(false), ScheduledEnabled: policyTestPtr(false)})
	_, err := UpsertPolicy(context.Background(), db, "openai", "", input, 1)
	if !errors.Is(err, errPolicyTestStop) {
		t.Fatalf("保存未走到写入：%v", err)
	}
	if got := db.savedFlows(t); !reflect.DeepEqual(got, want) {
		t.Fatalf("顶层补丁覆盖了已有 flows：got %+v want %+v", got, want)
	}
}

func TestUpsertPolicyRejectsDisabledThirdPartyProvider(t *testing.T) {
	flows := policyFlowsFixture()
	db := newPolicyTestDB(mustJSON(flows))
	sites := []ThirdPartyCheck{{Provider: "bazaarlink"}}
	input := policyTestInput(t, PolicyInput{Flows: &flows, ThirdParty: &sites})
	_, err := UpsertPolicy(context.Background(), db, "openai", "", input, 1)
	var httpErr *httpx.Error
	if !errors.As(err, &httpErr) || httpErr.Code != "THIRD_PARTY_REQUIRED" {
		t.Fatalf("停用全部已配置检测站后应拒绝保存：%v", err)
	}
	if db.inserted != nil {
		t.Fatal("没有已启用检测站的策略不应写入数据库")
	}
}

func TestUpsertPolicyRejectsExplicitZeroDegradation(t *testing.T) {
	db := newPolicyTestDB([]byte(`{}`))
	flows := policyFlowsFixture()
	flows.Identity.Enabled = false
	flows.Identity.Source = "self"
	flows.Degradation.Enabled = false
	flows.Degradation.IntervalMinutes = 0
	input := policyTestInput(t, PolicyInput{Flows: &flows})
	_, err := UpsertPolicy(context.Background(), db, "openai", "", input, 1)
	var httpErr *httpx.Error
	if !errors.As(err, &httpErr) || httpErr.Code != "INVALID_FLOW_INTERVAL" {
		t.Fatalf("显式 0 周期应被拒绝而不是回填：%v", err)
	}
	if db.inserted != nil {
		t.Fatal("无效周期不应写入数据库")
	}
}

func TestUpsertPolicyRejectsStoredZeroIntervalsWithoutLegacyOverwrite(t *testing.T) {
	flows := policyFlowsFixture()
	flows.Intelligence.IntervalMinutes = 0
	flows.Degradation.IntervalMinutes = 0
	flows.Identity.IntervalMinutes = 0
	db := newPolicyTestDB(mustJSON(flows))
	input := policyTestInput(t, PolicyInput{Enabled: policyTestPtr(false)})
	_, err := UpsertPolicy(context.Background(), db, "openai", "", input, 1)
	var httpErr *httpx.Error
	if !errors.As(err, &httpErr) || httpErr.Code != "INVALID_FLOW_INTERVAL" {
		t.Fatalf("已有自定义 flows 不应按空配置整体覆盖：%v", err)
	}
	if db.inserted != nil {
		t.Fatal("全 0 周期不应被 legacy 回填后写入")
	}
}

// 抖动在调度时按「流程周期的一半」钳位（engine.go scheduleDue），保存校验必须与它同口径：
// 以前拿顶层 interval_minutes 去比，而弹窗里根本没有这个输入框，管理员无从调整，报错还是英文。
func TestUpsertPolicyJitterMustFitShortestFlowInterval(t *testing.T) {
	flows := policyFlowsFixture()
	flows.Identity.Enabled = false
	flows.Identity.Source = "self"
	flows.Intelligence.IntervalMinutes = 10
	db := newPolicyTestDB(mustJSON(flows))
	input := policyTestInput(t, PolicyInput{Flows: &flows, JitterMinutes: policyTestPtr(30)})
	_, err := UpsertPolicy(context.Background(), db, "openai", "", input, 1)
	var httpErr *httpx.Error
	if !errors.As(err, &httpErr) || httpErr.Code != "INVALID_JITTER" {
		t.Fatalf("抖动超过最短流程周期的一半应被拒绝：%v", err)
	}
	if !strings.Contains(httpErr.Message, "抖动") || !strings.Contains(httpErr.Message, "10") {
		t.Fatalf("报错须点明字段与上限：%q", httpErr.Message)
	}
	if db.inserted != nil {
		t.Fatal("非法抖动不应写入数据库")
	}
}

func TestUpsertPolicyJitterAtHalfOfShortestIntervalIsAccepted(t *testing.T) {
	flows := policyFlowsFixture()
	flows.Identity.Enabled = false
	flows.Identity.Source = "self"
	flows.Intelligence.IntervalMinutes = 10
	db := newPolicyTestDB(mustJSON(flows))
	input := policyTestInput(t, PolicyInput{Flows: &flows, JitterMinutes: policyTestPtr(5)})
	_, err := UpsertPolicy(context.Background(), db, "openai", "", input, 1)
	if !errors.Is(err, errPolicyTestStop) {
		t.Fatalf("抖动等于最短周期一半应通过校验并进入写入：%v", err)
	}
}

// 顶层 interval_minutes 已是不再展示的历史字段：它不该再成为保存失败的原因。
func TestUpsertPolicyIgnoresLegacyTopLevelIntervalForJitter(t *testing.T) {
	flows := policyFlowsFixture()
	flows.Identity.Enabled = false
	flows.Identity.Source = "self"
	db := newPolicyTestDB(mustJSON(flows))
	// 顶层周期被历史数据设成 10：旧校验会因为 jitter(30) >= 10 拒绝，尽管三条流程周期都远大于 60。
	input := policyTestInput(t, PolicyInput{Flows: &flows, IntervalMinutes: policyTestPtr(10), JitterMinutes: policyTestPtr(30)})
	_, err := UpsertPolicy(context.Background(), db, "openai", "", input, 1)
	if !errors.Is(err, errPolicyTestStop) {
		t.Fatalf("三条流程周期都足够长时，顶层历史周期不应阻止保存：%v", err)
	}
}
