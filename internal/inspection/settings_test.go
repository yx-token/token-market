package inspection

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type memoryRow struct{ raw []byte }

func (row memoryRow) Scan(values ...any) error {
	if row.raw == nil {
		return pgx.ErrNoRows
	}
	*values[0].(*[]byte) = append([]byte(nil), row.raw...)
	return nil
}

type memoryDB struct{ raw []byte }

func (db *memoryDB) QueryRow(context.Context, string, ...any) pgx.Row { return memoryRow{db.raw} }
func (db *memoryDB) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	if args[0] != "inspection" {
		return pgconn.CommandTag{}, errors.New("错误设置键")
	}
	db.raw = append([]byte(nil), args[1].([]byte)...)
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func TestSettingsLoadDefaultsAndPartialBudget(t *testing.T) {
	db := &memoryDB{}
	s, err := Load(context.Background(), db)
	if err != nil || s.Mode != ModeTrial || s.Limits.Account != 2 || s.Scheduling.Enabled {
		t.Fatalf("默认值错误：%+v %v", s, err)
	}
	db.raw = []byte(`{"token_budgets":{"full":{"input_tokens_per_side":42}},"limits":{"account_parallelism":1}}`)
	s, err = Load(context.Background(), db)
	if err != nil || s.TokenBudgets["full"].Input != 42 || s.TokenBudgets["full"].Output != 640000 || s.TokenBudgets["quick"].Input != 80000 || s.Limits.Global != 32 {
		t.Fatalf("部分设置清空默认值：%+v %v", s, err)
	}
}

func TestSettingsUnknownFieldsPreservedAndInactive(t *testing.T) {
	db := &memoryDB{raw: []byte(`{"future":{"version":3},"limits":{"future_account_rule":7},"check_overrides":{"D06":{"future_flag":"保留","coefficients":{"future":0.2}},"future_check":{"enabled":true,"calibrated":true}}}`)}
	s, err := Update(context.Background(), db, []byte(`{"mode":"formal","limits":{"account_parallelism":1}}`))
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	if err := json.Unmarshal(db.raw, &saved); err != nil {
		t.Fatal(err)
	}
	if saved["future"].(map[string]any)["version"] != float64(3) || saved["limits"].(map[string]any)["future_account_rule"] != float64(7) || saved["check_overrides"].(map[string]any)["D06"].(map[string]any)["future_flag"] != "保留" {
		t.Fatalf("未知字段被覆盖：%s", db.raw)
	}
	effective, _ := Effective("D06", s)
	if _, applied := effective.Coefficients["future"]; applied || effective.Calibrated {
		t.Fatal("未知覆盖值或启用绕过校准")
	}
	if _, known := Effective("future_check", s); known {
		t.Fatal("未知质检项生效")
	}
}

func TestSettingsCalibrationRegistration(t *testing.T) {
	db := &memoryDB{}
	enabled := true
	if _, err := UpdateOverride(context.Background(), db, "D06", CheckOverride{Enabled: &enabled, Calibrated: true}); err == nil {
		t.Fatal("未登记校准记录仍允许标记已校准")
	}
	if s, err := UpdateOverride(context.Background(), db, "D06", CheckOverride{Enabled: &enabled}); err != nil {
		t.Fatal(err)
	} else if effective, _ := Effective("D06", s); effective.Calibrated {
		t.Fatal("enabled 自动变成 calibrated")
	}
	if s, err := UpdateOverride(context.Background(), db, "D06", CheckOverride{Enabled: &enabled, Calibrated: true, CalibrationNote: "型号/入口/网关版本/样本/误报/记录标识已登记"}); err != nil {
		t.Fatal(err)
	} else if effective, _ := Effective("D06", s); !effective.Calibrated {
		t.Fatal("独立校准记录未生效")
	}
	if _, err := UpdateOverride(context.Background(), db, "unknown", CheckOverride{}); err == nil {
		t.Fatal("独立项编辑允许未知目录键")
	}
}

func TestSettingsValidation(t *testing.T) {
	for _, patch := range []string{
		`{"mode":"shadow"}`, `{"grade_thresholds":{"B":0.99}}`, `{"grade_hysteresis":0.5}`, `{"downgrade_confirmations":1}`,
		`{"limits":{"account_parallelism":33}}`, `{"token_budgets":{"full":{"input_tokens_per_side":0}}}`,
		`{"check_overrides":{"D06":{"coefficients":{"warning":0}}}}`, `{"check_overrides":{"D07":{"thresholds":{"min_pairs":1.5}}}}`,
		`{"check_overrides":{"D04":{"thresholds":{"warning_rate":1.2}}}}`,
	} {
		t.Run(patch, func(t *testing.T) {
			if _, err := Update(context.Background(), &memoryDB{}, []byte(patch)); err == nil {
				t.Fatal("无效设置被接受")
			}
		})
	}
	if _, err := Update(context.Background(), &memoryDB{}, []byte(`{"check_overrides":{"D10":{"thresholds":{"warning_ratio":1.5,"severe_ratio":2}}}}`)); err != nil {
		t.Fatal("性能倍率不能按 fraction 拒绝：", err)
	}
}

func TestRegistryCopiesAndDefaultDisabledFingerprint(t *testing.T) {
	known := Known()
	if len(known) < 20 {
		t.Fatal("目录缺少约定质检项")
	}
	for i, definition := range known {
		if definition.Name == "" || definition.Description == "" || (i > 0 && known[i-1].Key >= definition.Key) {
			t.Fatal("目录说明缺失或顺序不稳定")
		}
	}
	definition, _ := Lookup("D06")
	definition.Coefficients["warning"] = 0.5
	unchanged, _ := Lookup("D06")
	if unchanged.Coefficients["warning"] != 0.85 {
		t.Fatal("调用方改写代码目录")
	}
	a15, _ := Effective("A15", Defaults())
	if a15.Enabled || a15.Calibrated {
		t.Fatal("参数指纹默认启用或校准")
	}
	s := Defaults()
	enabled := true
	s.CheckOverrides["A15"] = CheckOverride{Enabled: &enabled}
	a15, _ = Effective("A15", s)
	if a15.Enabled || a15.Calibrated {
		t.Fatal("参数指纹在未校准时被 enabled 激活")
	}
}
