package listing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/DueGin/token-market/internal/platform/httpx"
	"github.com/DueGin/token-market/internal/sub2api/adminclient"
)

// UpstreamModels 保留目录与卖家选择的区别：白名单为空值时，存量账号不限制模型。
type UpstreamModels struct {
	Catalog   []string   `json:"catalog"`
	Whitelist []string   `json:"whitelist"`
	FetchedAt *time.Time `json:"fetched_at"`
}

func upstreamModels(u *Upstream) *UpstreamModels {
	return &UpstreamModels{Catalog: u.ModelCatalog, Whitelist: u.ModelWhitelist, FetchedAt: u.ModelCatalogAt}
}

func (s *Service) GetModels(ctx context.Context, sellerID, upstreamID int64) (*UpstreamModels, error) {
	u, err := GetSellerUpstream(ctx, s.DB, sellerID, upstreamID)
	if err != nil {
		return nil, err
	}
	return upstreamModels(u), nil
}

// lockAccountModels 用连接级锁串行化白名单和重新授权，外部请求不占用数据库事务。
// 与导入路径已有的上游锁使用同一个键，防止选择过程覆盖尚未完成的导入。
func (s *Service) lockAccountModels(ctx context.Context, upstreamID int64) (func(), error) {
	conn, err := s.DB.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	var locked bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", -upstreamID).Scan(&locked); err != nil {
		_ = conn.Conn().Close(context.WithoutCancel(ctx))
		conn.Release()
		return nil, err
	}
	if !locked {
		conn.Release()
		return nil, httpx.Conflict("UPSTREAM_BUSY", "上游正在处理其他操作，请稍后重试")
	}
	return func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(cleanup, "SELECT pg_advisory_unlock($1)", -upstreamID); err != nil {
			// 解锁失败的连接不能回到池中，否则后续请求可能继承这把锁。
			_ = conn.Conn().Close(cleanup)
		}
		conn.Release()
	}, nil
}

func modelsEditable(u *Upstream) bool {
	return u.Sub2APIAccountID != nil && u.Status != StatusRemoving && u.Status != StatusRemoved && u.Status != StatusFailed
}

func (s *Service) RefreshModels(ctx context.Context, sellerID, upstreamID int64) (*UpstreamModels, error) {
	release, err := s.lockAccountModels(ctx, upstreamID)
	if err != nil {
		return nil, err
	}
	defer release()
	u, err := GetSellerUpstream(ctx, s.DB, sellerID, upstreamID)
	if err != nil {
		return nil, err
	}
	if !modelsEditable(u) {
		return nil, ErrBadState
	}
	if err := s.fetchModelCatalog(ctx, u); err != nil {
		return nil, err
	}
	return upstreamModels(u), nil
}

func (s *Service) fetchModelCatalog(ctx context.Context, u *Upstream) error {
	catalog, fromUpstream, err := s.Admin.GetAccountModelCatalog(ctx, *u.Sub2APIAccountID)
	if err != nil {
		return httpx.Unavailable("MODEL_CATALOG_UNAVAILABLE", "暂时无法获取模型目录，请稍后刷新或手动填写模型 ID")
	}
	if !fromUpstream && u.ModelWhitelist != nil {
		// GET 可能只返回映射内模型，不能用它抹掉写白名单之前缓存的完整目录。
		seen := make(map[string]bool, len(u.ModelCatalog)+len(catalog))
		complete := make([]string, 0, len(u.ModelCatalog)+len(catalog))
		for _, model := range append(append([]string{}, u.ModelCatalog...), catalog...) {
			if !seen[model] {
				complete = append(complete, model)
				seen[model] = true
			}
		}
		catalog = complete
	}
	if catalog == nil {
		catalog = []string{}
	}
	at := s.now()
	tag, err := s.DB.Exec(ctx, `UPDATE upstreams SET model_catalog = $2, model_catalog_at = $3, updated_at = now()
		WHERE id = $1 AND sub2api_account_id = $4 AND status NOT IN ('removing', 'removed')`, u.ID, catalog, at, *u.Sub2APIAccountID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrBadState
	}
	u.ModelCatalog, u.ModelCatalogAt = catalog, &at
	return nil
}

func validateModelWhitelist(models, catalog []string) ([]string, error) {
	if len(models) == 0 {
		return nil, httpx.BadRequest("MODEL_WHITELIST_REQUIRED", "请至少选择一个挂售模型")
	}
	available := make(map[string]bool, len(catalog))
	for _, model := range catalog {
		available[model] = true
	}
	selected := make([]string, 0, len(models))
	seen := make(map[string]bool, len(models))
	for _, value := range models {
		model := strings.TrimSpace(value)
		// sub2api 的映射键支持通配符，挂售白名单只接受具体 ID，防止扩大接单范围。
		if model == "" || strings.Contains(model, "*") || strings.IndexFunc(model, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
			return nil, httpx.BadRequest("INVALID_MODEL", "模型 ID 不能为空，也不能包含空白字符或通配符")
		}
		if len(catalog) > 0 && !available[model] {
			return nil, httpx.BadRequest("MODEL_NOT_IN_CATALOG", fmt.Sprintf("模型 %s 不在该账号的模型目录中", model))
		}
		if !seen[model] {
			selected = append(selected, model)
			seen[model] = true
		}
	}
	return selected, nil
}

func modelMapping(models []string) map[string]string {
	mapping := make(map[string]string, len(models))
	for _, model := range models {
		mapping[model] = model
	}
	return mapping
}

// SetModels 先确认 sub2api 已接受白名单，再保存并推进待选择账号的上架检测。
func (s *Service) SetModels(ctx context.Context, sellerID, upstreamID int64, models []string) (*Upstream, error) {
	release, err := s.lockAccountModels(ctx, upstreamID)
	if err != nil {
		return nil, err
	}
	defer release()
	u, err := GetSellerUpstream(ctx, s.DB, sellerID, upstreamID)
	if err != nil {
		return nil, err
	}
	if !modelsEditable(u) {
		return nil, ErrBadState
	}
	selected, err := validateModelWhitelist(models, u.ModelCatalog)
	if err != nil {
		return nil, err
	}
	if err := s.Admin.SetAccountModelWhitelist(ctx, *u.Sub2APIAccountID, selected); err != nil {
		return nil, fmt.Errorf("同步挂售模型白名单：%w", err)
	}
	tag, err := s.DB.Exec(ctx, `UPDATE upstreams SET model_whitelist = $2, updated_at = now()
		WHERE id = $1 AND seller_id = $3 AND sub2api_account_id = $4 AND status = $5`, u.ID, selected, sellerID, *u.Sub2APIAccountID, u.Status)
	if err != nil {
		return nil, errors.Join(err, s.restoreModelMapping(ctx, u))
	}
	if tag.RowsAffected() != 1 {
		return nil, errors.Join(ErrBadState, s.restoreModelMapping(ctx, u))
	}
	u.ModelWhitelist = selected
	if u.Status == StatusSelectingModels {
		return s.startVerification(ctx, u)
	}
	s.kick(ctx)
	return GetUpstream(ctx, s.DB, u.ID)
}

// 本地写入失败时恢复旧映射，避免 sub2api 与市场展示产生持久分歧；重试提交仍然幂等。
func (s *Service) restoreModelMapping(ctx context.Context, u *Upstream) error {
	restoreCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if u.ModelWhitelist != nil {
		if err := s.Admin.SetAccountModelWhitelist(restoreCtx, *u.Sub2APIAccountID, u.ModelWhitelist); err != nil {
			return fmt.Errorf("恢复原挂售模型映射失败：%w", err)
		}
		return nil
	}
	_, err := s.Admin.BulkUpdateAccounts(restoreCtx, adminclient.BulkUpdateAccountsInput{
		AccountIDs: []int64{*u.Sub2APIAccountID}, Credentials: map[string]any{"model_mapping": modelMapping(u.ModelWhitelist)},
	})
	if err != nil {
		return fmt.Errorf("恢复原挂售模型映射失败：%w", err)
	}
	return nil
}
