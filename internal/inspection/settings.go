package inspection

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const settingsKey = "inspection"

type DB interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func Defaults() Settings {
	s := Settings{
		Mode: ModeTrial, CheckOverrides: map[string]CheckOverride{},
		GradeThresholds: map[string]float64{"S": 1.02, "A": 0.93, "B": 0.82, "C": 0.65},
		GradeHysteresis: 0.02, DowngradeConfirmations: 2,
		TokenBudgets: map[string]TokenBudget{"full": {Input: 1300000, Output: 640000}, "quick": {Input: 80000, Output: 160000}},
	}
	s.SRequirements.GateFreeDays, s.SRequirements.AvailabilityDays, s.SRequirements.MinAvailability = 30, 7, 0.995
	s.Scheduling.MinimumGrade, s.Scheduling.Strategy = "B", "quality_adjusted"
	s.Timeouts.Full, s.Timeouts.Quick = 75, 20
	s.Limits.Account, s.Limits.Global = 2, 32
	return s
}

func (s Settings) Validate() error {
	if s.Mode != ModeTrial && s.Mode != ModeFormal {
		return errors.New("质检模式须为 trial 或 formal")
	}
	previous := 0.5
	for _, grade := range []string{"C", "B", "A", "S"} {
		value, ok := s.GradeThresholds[grade]
		if !ok || !finite(value) || value <= previous || value > 1.05 {
			return errors.New("质检等级分界须按 C、B、A、S 严格递增且处于 (0.5, 1.05]")
		}
		previous = value
	}
	if !finite(s.GradeHysteresis) || s.GradeHysteresis < 0 || s.GradeHysteresis > 0.1 || s.DowngradeConfirmations < 2 || s.DowngradeConfirmations > 10 {
		return errors.New("质检滞回须处于 [0, 0.1]，降级确认次数须处于 [2, 10]")
	}
	if s.SRequirements.GateFreeDays < 1 || s.SRequirements.GateFreeDays > 3650 || s.SRequirements.AvailabilityDays < 1 || s.SRequirements.AvailabilityDays > 365 || !finite(s.SRequirements.MinAvailability) || s.SRequirements.MinAvailability <= 0 || s.SRequirements.MinAvailability > 1 {
		return errors.New("质检 S 级历史要求的天数或可用率无效")
	}
	if GradeRank(s.Scheduling.MinimumGrade) < 0 || s.Scheduling.Strategy != "quality_adjusted" {
		return errors.New("保障池最低等级或质检调度策略无效")
	}
	if s.Limits.Account < 1 || s.Limits.Global < s.Limits.Account || s.Limits.Global > 256 || s.Timeouts.Full < 1 || s.Timeouts.Full > 1440 || s.Timeouts.Quick < 1 || s.Timeouts.Quick > 1440 {
		return errors.New("质检并发或截止时间配置无效")
	}
	for _, mode := range []string{"full", "quick"} {
		budget, ok := s.TokenBudgets[mode]
		if !ok || budget.Input <= 0 || budget.Output <= 0 {
			return errors.New("质检 token 预算须为正整数")
		}
	}
	for key, override := range s.CheckOverrides {
		definition, known := Lookup(key)
		if !known {
			continue
		}
		if override.Calibrated && strings.TrimSpace(override.CalibrationNote) == "" {
			return fmt.Errorf("质检项 %s 标记已校准时须登记型号、入口、网关版本、样本、误报和记录标识", key)
		}
		if len(override.CalibrationNote) > 4096 {
			return fmt.Errorf("质检项 %s 的校准记录不能超过 4096 字节", key)
		}
		for parameter, value := range override.Coefficients {
			if _, allowed := definition.Coefficients[parameter]; allowed && (!finite(value) || value <= 0 || value > 1.05) {
				return fmt.Errorf("质检项 %s 系数 %s 须处于 (0, 1.05]", key, parameter)
			}
		}
		for parameter, value := range override.Thresholds {
			if _, allowed := definition.Thresholds[parameter]; !allowed {
				continue
			}
			if !finite(value) || value < 0 || value > 10000000 {
				return fmt.Errorf("质检项 %s 阈值 %s 超出范围", key, parameter)
			}
			if strings.Contains(parameter, "rate") || strings.Contains(parameter, "relative") || parameter == "alpha" || parameter == "confidence" || (key == "D06" && (strings.Contains(parameter, "ratio") || parameter == "ci_upper")) {
				if value > 1 {
					return fmt.Errorf("质检项 %s 比例阈值 %s 须处于 [0, 1]", key, parameter)
				}
			}
			if (strings.Contains(parameter, "samples") || strings.Contains(parameter, "pairs")) && (value < 1 || value != math.Trunc(value)) {
				return fmt.Errorf("质检项 %s 样本下限须为正整数", key)
			}
		}
	}
	return nil
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

// 默认、已保存对象与本次补丁逐层合并，未知字段原样保留但不参与规则计算。
func mergeJSON(base, patch []byte) ([]byte, error) {
	if !json.Valid(patch) {
		return nil, errors.New("质检设置必须是有效 JSON")
	}
	if len(bytes.TrimSpace(patch)) == 0 || bytes.TrimSpace(patch)[0] != '{' {
		return append([]byte(nil), patch...), nil
	}
	var left, right map[string]json.RawMessage
	if err := json.Unmarshal(base, &left); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(patch, &right); err != nil {
		return nil, err
	}
	if left == nil {
		left = map[string]json.RawMessage{}
	}
	for key, value := range right {
		old := left[key]
		if len(bytes.TrimSpace(old)) > 0 && bytes.TrimSpace(old)[0] == '{' && len(bytes.TrimSpace(value)) > 0 && bytes.TrimSpace(value)[0] == '{' {
			merged, err := mergeJSON(old, value)
			if err != nil {
				return nil, err
			}
			left[key] = merged
		} else {
			left[key] = value
		}
	}
	return json.Marshal(left)
}

func loadRaw(ctx context.Context, db DB) ([]byte, error) {
	raw, err := json.Marshal(Defaults())
	if err != nil {
		return nil, err
	}
	var saved []byte
	err = db.QueryRow(ctx, "SELECT value FROM settings WHERE key = $1", settingsKey).Scan(&saved)
	if errors.Is(err, pgx.ErrNoRows) {
		return raw, nil
	}
	if err != nil {
		return nil, err
	}
	return mergeJSON(raw, saved)
}

func decode(raw []byte) (Settings, error) {
	var s Settings
	if err := json.Unmarshal(raw, &s); err != nil {
		return s, fmt.Errorf("解析质检设置：%w", err)
	}
	return s, s.Validate()
}

func Load(ctx context.Context, db DB) (Settings, error) {
	raw, err := loadRaw(ctx, db)
	if err != nil {
		return Settings{}, err
	}
	return decode(raw)
}

func Update(ctx context.Context, db DB, patch json.RawMessage) (Settings, error) {
	if len(bytes.TrimSpace(patch)) == 0 || bytes.TrimSpace(patch)[0] != '{' {
		return Settings{}, errors.New("质检设置补丁须为对象")
	}
	raw, err := loadRaw(ctx, db)
	if err != nil {
		return Settings{}, err
	}
	raw, err = mergeJSON(raw, patch)
	if err != nil {
		return Settings{}, err
	}
	s, err := decode(raw)
	if err != nil {
		return s, err
	}
	_, err = db.Exec(ctx, `INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value, updated_at=now()`, settingsKey, raw)
	return s, err
}

func UpdateOverride(ctx context.Context, db DB, key string, override CheckOverride) (Settings, error) {
	if _, ok := Lookup(key); !ok {
		return Settings{}, fmt.Errorf("未知质检项 %s", key)
	}
	patch, err := json.Marshal(map[string]any{"check_overrides": map[string]CheckOverride{key: override}})
	if err != nil {
		return Settings{}, err
	}
	return Update(ctx, db, patch)
}
