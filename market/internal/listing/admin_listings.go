package listing

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/yx-token/token-market/market/internal/platform/httpx"
)

// AdminListingRow 管理端列表行（附上游平台与卖家账号）。
type AdminListingRow struct {
	Listing
	Platform    string `json:"platform"`
	SellerEmail string `json:"seller_email"`
}

// AdminListFilter 管理端挂单筛选。
type AdminListFilter struct {
	Status     string
	SellerID   int64
	UpstreamID int64
	Platform   string
	Q          string
	Limit      int
	Offset     int
}

// AdminUpdateListingInput 管理端可写字段。
type AdminUpdateListingInput struct {
	Status          *string  `json:"status"`
	Multiplier      *float64 `json:"multiplier"`
	JoinDefaultPool *bool    `json:"join_default_pool"`
	Title           *string  `json:"title"`
	Description     *string  `json:"description"`
	Categories      []string `json:"categories"`
	SKUKind         *string  `json:"sku_kind"`
	FaceValue       *float64 `json:"face_value"`
	Stock           *int     `json:"stock"`
	MinAmount       *float64 `json:"min_amount"`
}

// AdminCreateListingInput 代卖家创建：指定上游即可。
type AdminCreateListingInput struct {
	UpstreamID int64 `json:"upstream_id"`
	CreateListingInput
}

// AdminListListings 跨卖家列出挂单。
func (s *Service) AdminListListings(ctx context.Context, f AdminListFilter) ([]AdminListingRow, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	var b strings.Builder
	args := make([]any, 0, 8)
	b.WriteString(`SELECT l.id, l.upstream_id, l.seller_id, l.mode, l.status, l.paused_by, l.multiplier, l.join_default_pool,
		l.listing_group_id, l.listing_group_rate, l.price_changed_at, l.sku_kind, l.face_value, l.stock, l.stock_sold, l.min_amount,
		l.title, l.description, COALESCE(l.categories, '{}'), l.created_at, l.updated_at, l.closed_at, u.platform,
		COALESCE(usr.email, '')
		FROM listings l
		JOIN upstreams u ON u.id = l.upstream_id
		LEFT JOIN users usr ON usr.id = l.seller_id
		WHERE 1=1`)
	if f.Status != "" {
		args = append(args, f.Status)
		fmt.Fprintf(&b, " AND l.status = $%d", len(args))
	}
	if f.SellerID > 0 {
		args = append(args, f.SellerID)
		fmt.Fprintf(&b, " AND l.seller_id = $%d", len(args))
	}
	if f.UpstreamID > 0 {
		args = append(args, f.UpstreamID)
		fmt.Fprintf(&b, " AND l.upstream_id = $%d", len(args))
	}
	if f.Platform != "" {
		args = append(args, f.Platform)
		fmt.Fprintf(&b, " AND u.platform = $%d", len(args))
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		if id, err := strconv.ParseInt(q, 10, 64); err == nil {
			args = append(args, id)
			p := len(args)
			fmt.Fprintf(&b, " AND (l.id = $%d OR l.upstream_id = $%d OR l.seller_id = $%d)", p, p, p)
		} else {
			args = append(args, "%"+strings.ToLower(q)+"%")
			p := len(args)
			fmt.Fprintf(&b, " AND (lower(coalesce(l.title,'')) LIKE $%d OR lower(coalesce(l.description,'')) LIKE $%d OR lower(coalesce(usr.email,'')) LIKE $%d)", p, p, p)
		}
	}
	args = append(args, f.Limit, f.Offset)
	fmt.Fprintf(&b, " ORDER BY l.id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))

	rows, err := s.DB.Query(ctx, b.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AdminListingRow, 0)
	for rows.Next() {
		var row AdminListingRow
		l := &row.Listing
		if err := rows.Scan(&l.ID, &l.UpstreamID, &l.SellerID, &l.Mode, &l.Status, &l.PausedBy, &l.Multiplier, &l.JoinDefaultPool,
			&l.ListingGroupID, &l.ListingGroupRate, &l.PriceChangedAt, &l.SKUKind, &l.FaceValue, &l.Stock, &l.StockSold, &l.MinAmount,
			&l.Title, &l.Description, &l.Categories, &l.CreatedAt, &l.UpdatedAt, &l.ClosedAt, &row.Platform, &row.SellerEmail); err != nil {
			return nil, err
		}
		if l.Categories == nil {
			l.Categories = []string{}
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// AdminGetListing 管理端详情。
func (s *Service) AdminGetListing(ctx context.Context, id int64) (*AdminListingRow, error) {
	l, err := GetListing(ctx, s.DB, id)
	if err != nil {
		return nil, err
	}
	u, err := GetUpstream(ctx, s.DB, l.UpstreamID)
	if err != nil {
		return nil, err
	}
	return &AdminListingRow{Listing: *l, Platform: u.Platform, SellerEmail: s.sellerEmail(ctx, l.SellerID)}, nil
}

// AdminCreateListing 代卖家创建挂单（复用 CreateListing 门禁）。
func (s *Service) AdminCreateListing(ctx context.Context, in AdminCreateListingInput) (*Listing, *Quote, error) {
	if in.UpstreamID <= 0 {
		return nil, nil, httpx.BadRequest("UPSTREAM_REQUIRED", "upstream_id is required")
	}
	u, err := GetUpstream(ctx, s.DB, in.UpstreamID)
	if err != nil {
		return nil, nil, err
	}
	return s.CreateListing(ctx, u.SellerID, u.ID, in.CreateListingInput)
}

// AdminUpdateListing 更新业务字段（不改 seller/upstream）。
func (s *Service) AdminUpdateListing(ctx context.Context, id int64, in AdminUpdateListingInput) (*Listing, error) {
	l, err := GetListing(ctx, s.DB, id)
	if err != nil {
		return nil, err
	}
	if l.Status == ListingClosed {
		return nil, ErrBadState
	}
	sets := make([]string, 0, 12)
	args := make([]any, 0, 12)
	push := func(col string, v any) {
		args = append(args, v)
		sets = append(sets, fmt.Sprintf("%s = $%d", col, len(args)))
	}
	if in.Status != nil {
		st := strings.TrimSpace(*in.Status)
		switch st {
		case ListingActive, ListingPaused, ListingPendingConfirm:
			push("status", st)
			if st == ListingActive {
				push("paused_by", nil)
			}
		default:
			return nil, httpx.BadRequest("INVALID_STATUS", "status must be active, paused, or pending_confirm")
		}
	}
	if in.Multiplier != nil {
		push("multiplier", round4(*in.Multiplier))
		push("price_changed_at", time.Now())
	}
	if in.JoinDefaultPool != nil {
		push("join_default_pool", *in.JoinDefaultPool)
	}
	if in.Title != nil {
		t := strings.TrimSpace(*in.Title)
		if t == "" {
			push("title", nil)
		} else {
			push("title", t)
		}
	}
	if in.Description != nil {
		d := strings.TrimSpace(*in.Description)
		if d == "" {
			push("description", nil)
		} else {
			push("description", d)
		}
	}
	if in.Categories != nil {
		allowed, err := sellerPickableSet(ctx, s.DB)
		if err != nil {
			return nil, err
		}
		cats, err := NormalizeCategories(l.Mode, in.Categories, allowed)
		if err != nil {
			return nil, err
		}
		push("categories", cats)
	}
	if in.SKUKind != nil {
		push("sku_kind", strings.TrimSpace(*in.SKUKind))
	}
	if in.FaceValue != nil {
		push("face_value", round2(*in.FaceValue))
	}
	if in.Stock != nil {
		push("stock", *in.Stock)
	}
	if in.MinAmount != nil {
		push("min_amount", round2(*in.MinAmount))
	}
	if len(sets) == 0 {
		return l, nil
	}
	args = append(args, id)
	q := fmt.Sprintf("UPDATE listings SET %s, updated_at = now() WHERE id = $%d", strings.Join(sets, ", "), len(args))
	if _, err := s.DB.Exec(ctx, q, args...); err != nil {
		return nil, err
	}
	if in.Multiplier != nil {
		st, err := s.Settings(ctx)
		if err == nil && *in.Multiplier > 0 {
			_, _ = s.DB.Exec(ctx, `INSERT INTO listing_price_versions (listing_id, multiplier, buyer_price, source) VALUES ($1, $2, $3, 'admin')`,
				id, round4(*in.Multiplier), BuyerPrice(round4(*in.Multiplier), st.CommissionRate))
		}
	}
	s.kick(ctx)
	return GetListing(ctx, s.DB, id)
}

// AdminPauseListing 强制暂停。
func (s *Service) AdminPauseListing(ctx context.Context, id int64) (*Listing, error) {
	l, err := GetListing(ctx, s.DB, id)
	if err != nil {
		return nil, err
	}
	if l.Status != ListingActive {
		return nil, ErrBadState
	}
	if _, err := s.DB.Exec(ctx, `UPDATE listings SET status = 'paused', paused_by = 'admin', updated_at = now() WHERE id = $1`, l.ID); err != nil {
		return nil, err
	}
	s.kick(ctx)
	return GetListing(ctx, s.DB, l.ID)
}

// AdminResumeListing 强制恢复（含 admin 暂停）。
func (s *Service) AdminResumeListing(ctx context.Context, id int64) (*Listing, error) {
	l, err := GetListing(ctx, s.DB, id)
	if err != nil {
		return nil, err
	}
	if l.Status != ListingPaused {
		return nil, ErrBadState
	}
	if _, err := s.DB.Exec(ctx, `UPDATE listings SET status = 'active', paused_by = NULL, updated_at = now() WHERE id = $1`, l.ID); err != nil {
		return nil, err
	}
	s.kick(ctx)
	return GetListing(ctx, s.DB, l.ID)
}

// AdminCloseListing 强制关闭。
func (s *Service) AdminCloseListing(ctx context.Context, id int64) error {
	l, err := GetListing(ctx, s.DB, id)
	if err != nil {
		return err
	}
	return s.closeListing(ctx, l)
}
