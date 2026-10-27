// Package listing 管理卖家的上游（订阅账号）与挂单：上架授权、校验、寄售与回收两种挂单模式。
package listing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
)

// 上游状态
const (
	StatusDraft           = "draft"
	StatusAuthorizing     = "authorizing"
	StatusSelectingModels = "selecting_models"
	StatusVerifying       = "verifying"
	StatusVerifyFailed    = "verify_failed"
	StatusReady           = "ready"
	StatusListed          = "listed"
	StatusSuspended       = "suspended"
	StatusFailed          = "failed"
	StatusRemoving        = "removing"
	StatusRemoved         = "removed"
)

// 健康状态
const (
	HealthUnknown     = "unknown"
	HealthHealthy     = "healthy"
	HealthLimited     = "limited"
	HealthSuspected   = "suspected"
	HealthDegraded    = "degraded"
	HealthUnavailable = "unavailable"
)

// 挂单模式与状态
const (
	ModeConsignment = "consignment"
	ModeBuyback     = "buyback"
	ModePack        = "pack" // 额度包：固定面额、可售份数，不进保障池

	ListingPendingQuote   = "pending_quote"
	ListingPendingConfirm = "pending_confirm"
	ListingActive         = "active"
	ListingPaused         = "paused"
	ListingClosed         = "closed"
)

type VendorConfig struct {
	Platform           string             `json:"platform"`
	Enabled            bool               `json:"enabled"`
	AuthTypes          []string           `json:"auth_types"`           // 开放给卖家的接入方式
	SupportedAuthTypes []string           `json:"supported_auth_types"` // 本系统实现了的全部接入方式（不存库）
	ConsignmentMin     float64            `json:"consignment_min"`
	ConsignmentMax     float64            `json:"consignment_max"`
	BuybackBase        map[string]float64 `json:"buyback_base"`
	AppraisalBase      map[string]float64 `json:"appraisal_base"`
	DefaultConcurrency int                `json:"default_concurrency"`
	ExtraLimits        map[string]any     `json:"extra_limits"`
	UpdatedAt          time.Time          `json:"updated_at"`
}

// SupportsAuthType 判断厂商是否支持某种接入方式。
func (v VendorConfig) SupportsAuthType(t string) bool {
	for _, a := range v.AuthTypes {
		if a == t {
			return true
		}
	}
	return false
}

// BuybackBaseFor 返回某订阅档位的回收基础倍率，没有专门配置时用 default。
func (v VendorConfig) BuybackBaseFor(tier string) float64 {
	if b, ok := v.BuybackBase[tier]; ok && tier != "" {
		return b
	}
	return v.BuybackBase["default"]
}

// AppraisalBaseFor 只读取独立的质检估价基准，缺配置时不回退到回收报价。
func (v VendorConfig) AppraisalBaseFor(tier string) (float64, bool) {
	base, ok := v.AppraisalBase[tier]
	if tier == "" || !ok {
		base, ok = v.AppraisalBase["default"]
	}
	return base, ok && base > 0 && !math.IsNaN(base) && !math.IsInf(base, 0)
}

type Upstream struct {
	InspectionView
	ID                 int64      `json:"id"`
	Code               string     `json:"code"` // 展示编号 YYYYMMDD+随机串，界面用此字段而非裸 id
	SellerID           int64      `json:"seller_id"`
	Platform           string     `json:"platform"`
	AuthType           string     `json:"auth_type"`
	IsReference        bool       `json:"is_reference"`
	Status             string     `json:"status"`
	Health             string     `json:"health"`
	HealthReason       *string    `json:"health_reason,omitempty"`
	HealthChangedAt    *time.Time `json:"health_changed_at,omitempty"`
	Sub2APIAccountID   *int64     `json:"-"`
	ProxyID            *int64     `json:"proxy_id,omitempty"`
	ProbeGroupID       *int64     `json:"-"`
	Tier               *string    `json:"tier,omitempty"`
	AccountEmail       *string    `json:"account_email,omitempty"` // 官方账号邮箱，授权或导入时从令牌信息中识别
	Name               *string    `json:"name,omitempty"`          // 运营展示名称
	Note               *string    `json:"note,omitempty"`          // 运营备注
	PrivacyOK          *bool      `json:"privacy_ok,omitempty"`
	SelfReservePercent int        `json:"self_reserve_percent"`
	ModelWhitelist     []string   `json:"model_whitelist"` // 空值表示存量上游未声明，不限制模型
	ModelCatalog       []string   `json:"model_catalog"`
	ModelCatalogAt     *time.Time `json:"model_catalog_at"`
	// 自动用卡（仅 Codex）：卖家授权后，窗口用量达到阈值时由平台使用重置卡；阈值为空表示跟随暂停线
	AutoResetEnabled      bool       `json:"auto_reset_enabled"`
	AutoReset5hPercent    *int       `json:"auto_reset_5h_percent,omitempty"`
	AutoReset7dPercent    *int       `json:"auto_reset_7d_percent,omitempty"`
	AutoResetAuthorizedAt *time.Time `json:"auto_reset_authorized_at,omitempty"`
	Grade                 *string    `json:"grade,omitempty"`
	FailureReason         *string    `json:"failure_reason,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

const upstreamColumns = `id, code, seller_id, platform, auth_type, is_reference, status, health, health_reason, health_changed_at,
	sub2api_account_id, proxy_id, probe_group_id, tier, account_email, name, note, privacy_ok, self_reserve_percent,
	auto_reset_enabled, auto_reset_5h_percent, auto_reset_7d_percent, auto_reset_authorized_at, grade, failure_reason, created_at, updated_at,
	model_whitelist, model_catalog, model_catalog_at`

func scanUpstream(row pgx.Row) (*Upstream, error) {
	var u Upstream
	err := row.Scan(&u.ID, &u.Code, &u.SellerID, &u.Platform, &u.AuthType, &u.IsReference, &u.Status, &u.Health, &u.HealthReason,
		&u.HealthChangedAt, &u.Sub2APIAccountID, &u.ProxyID, &u.ProbeGroupID, &u.Tier, &u.AccountEmail, &u.Name, &u.Note, &u.PrivacyOK,
		&u.SelfReservePercent, &u.AutoResetEnabled, &u.AutoReset5hPercent, &u.AutoReset7dPercent, &u.AutoResetAuthorizedAt,
		&u.Grade, &u.FailureReason, &u.CreatedAt, &u.UpdatedAt, &u.ModelWhitelist, &u.ModelCatalog, &u.ModelCatalogAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

type Listing struct {
	ID               int64      `json:"id"`
	UpstreamID       int64      `json:"upstream_id"`
	SellerID         int64      `json:"seller_id"`
	Mode             string     `json:"mode"`
	Status           string     `json:"status"`
	PausedBy         *string    `json:"paused_by,omitempty"`
	Multiplier       *float64   `json:"multiplier,omitempty"`
	JoinDefaultPool  bool       `json:"join_default_pool"`
	ListingGroupID   *int64     `json:"-"`
	ListingGroupRate *float64   `json:"-"`
	PriceChangedAt   *time.Time `json:"price_changed_at,omitempty"`
	SKUKind          string     `json:"sku_kind"`
	FaceValue        *float64   `json:"face_value,omitempty"`
	Stock            *int       `json:"stock,omitempty"`
	StockSold        int        `json:"stock_sold"`
	MinAmount        float64    `json:"min_amount"`
	Title            *string    `json:"title,omitempty"`
	Description      *string    `json:"description,omitempty"`
	Categories       []string   `json:"categories,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	ClosedAt         *time.Time `json:"closed_at,omitempty"`
}

const listingColumns = `id, upstream_id, seller_id, mode, status, paused_by, multiplier, join_default_pool,
	listing_group_id, listing_group_rate, price_changed_at, sku_kind, face_value, stock, stock_sold, min_amount,
	title, description, COALESCE(categories, '{}'), created_at, updated_at, closed_at`

func scanListing(row pgx.Row) (*Listing, error) {
	var l Listing
	err := row.Scan(&l.ID, &l.UpstreamID, &l.SellerID, &l.Mode, &l.Status, &l.PausedBy, &l.Multiplier, &l.JoinDefaultPool,
		&l.ListingGroupID, &l.ListingGroupRate, &l.PriceChangedAt, &l.SKUKind, &l.FaceValue, &l.Stock, &l.StockSold, &l.MinAmount,
		&l.Title, &l.Description, &l.Categories, &l.CreatedAt, &l.UpdatedAt, &l.ClosedAt)
	if err != nil {
		return nil, err
	}
	if l.Categories == nil {
		l.Categories = []string{}
	}
	return &l, nil
}

type Quote struct {
	ID         int64      `json:"id"`
	UpstreamID int64      `json:"upstream_id"`
	ListingID  *int64     `json:"listing_id,omitempty"`
	Grade      string     `json:"grade"`
	Multiplier float64    `json:"multiplier"`
	Status     string     `json:"status"`
	ValidUntil time.Time  `json:"valid_until"`
	CreatedAt  time.Time  `json:"created_at"`
	DecidedAt  *time.Time `json:"decided_at,omitempty"`
}

// DB 是 pgxpool.Pool 与 pgx.Tx 的公共子集。
type DB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

var (
	ErrUpstreamNotFound = httpx.NotFound("UPSTREAM_NOT_FOUND", "upstream not found")
	ErrListingNotFound  = httpx.NotFound("LISTING_NOT_FOUND", "listing not found")
	ErrVendorDisabled   = httpx.BadRequest("VENDOR_DISABLED", "this vendor is not open for listing")
)

func GetVendor(ctx context.Context, db DB, platform string) (*VendorConfig, error) {
	var v VendorConfig
	var base, appraisal, extra []byte
	err := db.QueryRow(ctx, `SELECT platform, enabled, auth_types, consignment_min, consignment_max, buyback_base,
		appraisal_base, default_concurrency, extra_limits, updated_at FROM vendor_configs WHERE platform = $1`, platform).
		Scan(&v.Platform, &v.Enabled, &v.AuthTypes, &v.ConsignmentMin, &v.ConsignmentMax, &base, &appraisal, &v.DefaultConcurrency, &extra, &v.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.NotFound("VENDOR_NOT_FOUND", "unknown vendor "+platform)
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(base, &v.BuybackBase); err != nil {
		return nil, fmt.Errorf("vendor %s buyback_base: %w", platform, err)
	}
	if err := json.Unmarshal(appraisal, &v.AppraisalBase); err != nil {
		return nil, fmt.Errorf("厂商 %s 质检估价基准：%w", platform, err)
	}
	if v.AppraisalBase == nil {
		v.AppraisalBase = map[string]float64{}
	}
	if err := json.Unmarshal(extra, &v.ExtraLimits); err != nil {
		return nil, fmt.Errorf("vendor %s extra_limits: %w", platform, err)
	}
	v.SupportedAuthTypes = SupportedAuthTypes(platform)
	return &v, nil
}

func ListVendors(ctx context.Context, db DB) ([]VendorConfig, error) {
	rows, err := db.Query(ctx, `SELECT platform FROM vendor_configs ORDER BY platform`)
	if err != nil {
		return nil, err
	}
	platforms, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	out := make([]VendorConfig, 0, len(platforms))
	for _, p := range platforms {
		v, err := GetVendor(ctx, db, p)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, nil
}

// UpdateVendorInput 是管理员修改厂商配置的可选字段。
type UpdateVendorInput struct {
	Enabled            *bool               `json:"enabled"`
	AuthTypes          *[]string           `json:"auth_types"`
	ConsignmentMin     *float64            `json:"consignment_min"`
	ConsignmentMax     *float64            `json:"consignment_max"`
	BuybackBase        *map[string]float64 `json:"buyback_base"`
	AppraisalBase      *map[string]float64 `json:"appraisal_base"`
	DefaultConcurrency *int                `json:"default_concurrency"`
	ExtraLimits        *map[string]any     `json:"extra_limits"`
}

func UpdateVendor(ctx context.Context, db DB, platform string, in UpdateVendorInput) (*VendorConfig, error) {
	v, err := GetVendor(ctx, db, platform)
	if err != nil {
		return nil, err
	}
	if in.Enabled != nil {
		v.Enabled = *in.Enabled
	}
	if in.AuthTypes != nil {
		// 只能在已实现的接入方式中选择，并按固定顺序保存
		want := map[string]bool{}
		for _, t := range *in.AuthTypes {
			if !slices.Contains(v.SupportedAuthTypes, t) {
				return nil, httpx.BadRequest("UNSUPPORTED_AUTH_TYPE", fmt.Sprintf("%s does not support %s", platform, t))
			}
			want[t] = true
		}
		v.AuthTypes = nil
		for _, t := range v.SupportedAuthTypes {
			if want[t] {
				v.AuthTypes = append(v.AuthTypes, t)
			}
		}
	}
	if len(v.AuthTypes) == 0 {
		return nil, httpx.BadRequest("NO_AUTH_TYPE", "at least one auth type must be open")
	}
	if in.ConsignmentMin != nil {
		v.ConsignmentMin = *in.ConsignmentMin
	}
	if in.ConsignmentMax != nil {
		v.ConsignmentMax = *in.ConsignmentMax
	}
	if in.BuybackBase != nil {
		v.BuybackBase = *in.BuybackBase
	}
	if in.AppraisalBase != nil {
		v.AppraisalBase = *in.AppraisalBase
	}
	if in.DefaultConcurrency != nil {
		v.DefaultConcurrency = *in.DefaultConcurrency
	}
	if in.ExtraLimits != nil {
		v.ExtraLimits = *in.ExtraLimits
	}
	if v.ConsignmentMin <= 0 || v.ConsignmentMax < v.ConsignmentMin {
		return nil, httpx.BadRequest("INVALID_RANGE", "consignment range must satisfy 0 < min <= max")
	}
	if v.DefaultConcurrency <= 0 {
		return nil, httpx.BadRequest("INVALID_CONCURRENCY", "default_concurrency must be > 0")
	}
	if _, ok := v.BuybackBase["default"]; !ok {
		return nil, httpx.BadRequest("INVALID_BUYBACK_BASE", "buyback_base must contain a default entry")
	}
	if err := validateAppraisalBase(v.AppraisalBase); err != nil {
		return nil, err
	}
	base, _ := json.Marshal(v.BuybackBase)
	appraisal, _ := json.Marshal(v.AppraisalBase)
	extra, _ := json.Marshal(v.ExtraLimits)
	_, err = db.Exec(ctx, `UPDATE vendor_configs SET enabled = $2, consignment_min = $3, consignment_max = $4,
		buyback_base = $5, default_concurrency = $6, extra_limits = $7, auth_types = $8, appraisal_base = $9, updated_at = now() WHERE platform = $1`,
		platform, v.Enabled, v.ConsignmentMin, v.ConsignmentMax, base, v.DefaultConcurrency, extra, v.AuthTypes, appraisal)
	if err != nil {
		return nil, err
	}
	return GetVendor(ctx, db, platform)
}

func validateAppraisalBase(base map[string]float64) error {
	for tier, value := range base {
		if strings.TrimSpace(tier) == "" || value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			return httpx.BadRequest("INVALID_APPRAISAL_BASE", "质检估价基准须为档位到正数倍率的映射")
		}
	}
	return nil
}

func GetUpstream(ctx context.Context, db DB, id int64) (*Upstream, error) {
	u, err := scanUpstream(db.QueryRow(ctx, "SELECT "+upstreamColumns+" FROM upstreams WHERE id = $1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUpstreamNotFound
	}
	if err != nil {
		return nil, err
	}
	views, err := LoadInspectionViews(ctx, db, []int64{id})
	if err != nil {
		return nil, err
	}
	u.InspectionView = views[id]
	return u, nil
}

// GetSellerUpstream 返回属于该卖家的上游。
func GetSellerUpstream(ctx context.Context, db DB, sellerID, id int64) (*Upstream, error) {
	u, err := GetUpstream(ctx, db, id)
	if err != nil {
		return nil, err
	}
	if u.SellerID != sellerID {
		return nil, ErrUpstreamNotFound
	}
	return u, nil
}

func ListSellerUpstreams(ctx context.Context, db DB, sellerID int64) ([]Upstream, error) {
	rows, err := db.Query(ctx, "SELECT "+upstreamColumns+" FROM upstreams WHERE seller_id = $1 AND status <> 'removed' ORDER BY id DESC", sellerID)
	if err != nil {
		return nil, err
	}
	return collectUpstreamsWithInspection(ctx, db, rows)
}

// ListUpstreams 管理后台列表，status 为空表示全部（不含已删除）。
func ListUpstreams(ctx context.Context, db DB, status string, limit, offset int) ([]Upstream, error) {
	rows, err := db.Query(ctx, "SELECT "+upstreamColumns+` FROM upstreams
		WHERE ($1 = '' AND status <> 'removed') OR status = $1 ORDER BY id DESC LIMIT $2 OFFSET $3`, status, limit, offset)
	if err != nil {
		return nil, err
	}
	return collectUpstreamsWithInspection(ctx, db, rows)
}

func collectUpstreams(rows pgx.Rows) ([]Upstream, error) {
	defer rows.Close()
	var out []Upstream
	for rows.Next() {
		u, err := scanUpstream(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

func collectUpstreamsWithInspection(ctx context.Context, db DB, rows pgx.Rows) ([]Upstream, error) {
	ups, err := collectUpstreams(rows)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, len(ups))
	for i := range ups {
		ids[i] = ups[i].ID
	}
	views, err := LoadInspectionViews(ctx, db, ids)
	if err != nil {
		return nil, err
	}
	for i := range ups {
		ups[i].InspectionView = views[ups[i].ID]
	}
	return ups, nil
}

func GetListing(ctx context.Context, db DB, id int64) (*Listing, error) {
	l, err := scanListing(db.QueryRow(ctx, "SELECT "+listingColumns+" FROM listings WHERE id = $1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrListingNotFound
	}
	return l, err
}

// OpenListing 返回上游当前未关闭的挂单，没有时返回 nil。
func OpenListing(ctx context.Context, db DB, upstreamID int64) (*Listing, error) {
	l, err := scanListing(db.QueryRow(ctx, "SELECT "+listingColumns+" FROM listings WHERE upstream_id = $1 AND closed_at IS NULL", upstreamID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return l, err
}

func setUpstreamStatus(ctx context.Context, db DB, id int64, status string, reason *string) error {
	_, err := db.Exec(ctx, `UPDATE upstreams SET status = $2, failure_reason = $3, updated_at = now() WHERE id = $1`, id, status, reason)
	return err
}

// SetHealth 更新上游健康状态，返回状态是否发生变化。
func SetHealth(ctx context.Context, db DB, id int64, health, reason string) (bool, error) {
	tag, err := db.Exec(ctx, `UPDATE upstreams SET health = $2, health_reason = NULLIF($3, ''), health_changed_at = now(), updated_at = now()
		WHERE id = $1 AND (health <> $2 OR COALESCE(health_reason, '') <> $3)`, id, health, reason)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func strPtr(s string) *string { return &s }
