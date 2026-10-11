package quality

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// TokenBudget 按单侧计算输入与合并输出（含推理）的硬上限。
type TokenBudget struct {
	Input  int64 `json:"input_tokens_per_side"`
	Output int64 `json:"output_tokens_per_side"`
}

type InspectionSettings struct {
	Mode     string `json:"mode"`
	Timeouts struct {
		Full  int `json:"full_minutes"`
		Quick int `json:"quick_minutes"`
	} `json:"timeouts"`
	Limits struct {
		Account int `json:"account_parallelism"`
		Global  int `json:"global_parallelism"`
	} `json:"limits"`
	TokenBudgets map[string]TokenBudget `json:"token_budgets"`
}

func defaultInspectionSettings() InspectionSettings {
	s := InspectionSettings{Mode: "trial", TokenBudgets: map[string]TokenBudget{
		"full": {Input: 1300000, Output: 640000}, "quick": {Input: 80000, Output: 160000},
	}}
	s.Timeouts.Full, s.Timeouts.Quick = 75, 20
	s.Limits.Account, s.Limits.Global = 2, 32
	return s
}

// 新设置独立存放；从默认对象覆盖，避免部分配置清空其余默认值。
func loadInspectionSettings(ctx context.Context, db DB) (InspectionSettings, error) {
	s := defaultInspectionSettings()
	var raw []byte
	err := db.QueryRow(ctx, "SELECT value FROM settings WHERE key = 'inspection'").Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return s, fmt.Errorf("解析质检设置：%w", err)
	}
	var patch struct {
		TokenBudgets map[string]json.RawMessage `json:"token_budgets"`
	}
	if err := json.Unmarshal(raw, &patch); err != nil {
		return s, err
	}
	defaults := defaultInspectionSettings()
	for mode, part := range patch.TokenBudgets {
		b := defaults.TokenBudgets[mode]
		if err := json.Unmarshal(part, &b); err != nil {
			return s, err
		}
		s.TokenBudgets[mode] = b
	}
	if s.Mode != "trial" && s.Mode != "formal" {
		return s, errors.New("质检模式须为 trial 或 formal")
	}
	if s.Limits.Account < 1 || s.Limits.Global < s.Limits.Account || s.Limits.Global > 256 || s.Timeouts.Full < 1 || s.Timeouts.Full > 1440 || s.Timeouts.Quick < 1 || s.Timeouts.Quick > 1440 {
		return s, errors.New("质检并发或截止时间配置无效")
	}
	for _, mode := range []string{"full", "quick"} {
		b, ok := s.TokenBudgets[mode]
		if !ok || b.Input <= 0 || b.Output <= 0 {
			return s, errors.New("质检 token 预算须为正整数")
		}
	}
	return s, nil
}
