package quality

import (
	"context"
	"github.com/yx-token/idlicat/market/internal/inspection"
)

// 执行引擎与估价、调度共同读取独立的 inspection 设置。
type TokenBudget = inspection.TokenBudget
type InspectionSettings = inspection.Settings

func defaultInspectionSettings() InspectionSettings { return inspection.Defaults() }
func loadInspectionSettings(ctx context.Context, db DB) (InspectionSettings, error) {
	return inspection.Load(ctx, db)
}
