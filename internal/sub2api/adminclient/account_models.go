package adminclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// SyncAccountModels 直接从账号上游同步模型目录，避免账号映射把目录缩成白名单。
// 响应结构对照 sub2api v0.2.14 的 UpstreamModelCatalog。
func (c *Client) SyncAccountModels(ctx context.Context, id int64) ([]string, error) {
	var out struct {
		Models []string `json:"models"`
	}
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/admin/accounts/%d/models/sync-upstream", id), nil, "", &out); err != nil {
		return nil, err
	}
	return normalizeAccountModels(out.Models), nil
}

// GetAccountModels 优先同步上游目录，失败时查询 sub2api 的账号可用模型列表。
func (c *Client) GetAccountModels(ctx context.Context, id int64) ([]string, error) {
	models, _, err := c.GetAccountModelCatalog(ctx, id)
	return models, err
}

// GetAccountModelCatalog 同时返回目录来源；回退查询可能只包含白名单内模型，
// 调用方可用来源标志保留此前缓存的完整目录。
func (c *Client) GetAccountModelCatalog(ctx context.Context, id int64) (models []string, fromUpstream bool, err error) {
	models, syncErr := c.SyncAccountModels(ctx, id)
	if syncErr == nil {
		return models, true, nil
	}
	var out []struct {
		ID string `json:"id"`
	}
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/admin/accounts/%d/models", id), nil, "", &out); err != nil {
		// 保留两个错误的文字，同时让错误分类以最终查询失败为准。
		return nil, false, fmt.Errorf("同步模型目录失败（%v），查询账号模型失败：%w", syncErr, err)
	}
	models = make([]string, 0, len(out))
	for _, model := range out {
		models = append(models, model.ID)
	}
	return normalizeAccountModels(models), false, nil
}

func normalizeAccountModels(models []string) []string {
	out := make([]string, 0, len(models))
	seen := make(map[string]bool, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" || seen[model] {
			continue
		}
		seen[model] = true
		out = append(out, model)
	}
	return out
}

// SetAccountModelWhitelist 按 jsonb 顶层合并写入恒等映射，保留账号的登录凭证。
// nil 表示存量账号尚未声明白名单，不修改；显式白名单必须至少包含一个模型。
func (c *Client) SetAccountModelWhitelist(ctx context.Context, id int64, models []string) error {
	if models == nil {
		return nil
	}
	if len(models) == 0 {
		return errors.New("adminclient：挂售模型白名单不能为空")
	}
	mapping := make(map[string]string, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			return errors.New("adminclient：挂售模型 ID 不能为空")
		}
		mapping[model] = model
	}
	raw, err := c.BulkUpdateAccounts(ctx, BulkUpdateAccountsInput{
		AccountIDs: []int64{id},
		Credentials: map[string]any{
			"model_mapping": mapping,
		},
	})
	if err != nil {
		return err
	}
	var result struct {
		Success    int     `json:"success"`
		Failed     int     `json:"failed"`
		SuccessIDs []int64 `json:"success_ids"`
		FailedIDs  []int64 `json:"failed_ids"`
		Results    []struct {
			AccountID int64  `json:"account_id"`
			Success   bool   `json:"success"`
			Error     string `json:"error"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("adminclient：解析挂售模型白名单写入结果：%w", err)
	}
	if result.Success != 1 || result.Failed != 0 || len(result.FailedIDs) != 0 {
		return fmt.Errorf("adminclient：账号 %d 的挂售模型白名单写入失败（成功 %d，失败 %d）", id, result.Success, result.Failed)
	}
	if len(result.SuccessIDs) > 0 {
		found := false
		for _, accountID := range result.SuccessIDs {
			found = found || accountID == id
		}
		if !found {
			return fmt.Errorf("adminclient：账号 %d 未出现在白名单写入成功列表", id)
		}
	}
	if len(result.Results) > 0 {
		found := false
		for _, entry := range result.Results {
			if entry.AccountID != id {
				continue
			}
			if !entry.Success {
				return fmt.Errorf("adminclient：账号 %d 的挂售模型白名单写入失败：%s", id, entry.Error)
			}
			found = true
		}
		if !found {
			return fmt.Errorf("adminclient：账号 %d 缺少白名单写入结果", id)
		}
	}
	return nil
}
