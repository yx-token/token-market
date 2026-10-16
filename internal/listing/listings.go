package listing

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/DueGin/token-market/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
)

// BuyerPrice 返回寄售挂单的买家价 p = m_s × (1 + c)，保留 4 位小数（sub2api 倍率精度）。
func BuyerPrice(multiplier, commission float64) float64 {
	return round4(multiplier * (1 + commission))
}

func round4(f float64) float64 { return math.Round(f*10000) / 10000 }
func round2(f float64) float64 { return math.Round(f*100) / 100 }

// PoolCap 返回某平台全部启用默认分组中最严格的倍率上限 D × (1 − 最低毛利 − 安全缓冲)。
// 没有默认分组时返回 0, false。
func PoolCap(ctx context.Context, db DB, platform string) (float64, bool, error) {
	var cap *float64
	err := db.QueryRow(ctx, `SELECT min(sell_multiplier * (1 - min_margin - safety_buffer)) FROM pools
		WHERE enabled AND (platform = $1 OR platform = 'composite')`, platform).Scan(&cap)
	if err != nil || cap == nil {
		return 0, false, err
	}
	return *cap, true, nil
}

// QuoteFor 计算回收报价：基础倍率(厂商, 档位) × 等级系数，且不超过默认分组的倍率上限。
func (s *Service) QuoteFor(ctx context.Context, u *Upstream) (grade string, multiplier float64, err error) {
	v, err := GetVendor(ctx, s.DB, u.Platform)
	if err != nil {
		return "", 0, err
	}
	st, err := s.Settings(ctx)
	if err != nil {
		return "", 0, err
	}
	grade = ""
	if u.Grade != nil {
		grade = *u.Grade
	}
	if grade == "" {
		grade, err = policyDefaultGrade(ctx, s.DB, u.Platform, u.AuthType)
		if err != nil {
			return "", 0, err
		}
	}
	tier := ""
	if u.Tier != nil {
		tier = *u.Tier
	}
	multiplier = v.BuybackBaseFor(tier) * st.GradeCoefficient(grade)
	if cap, ok, err := PoolCap(ctx, s.DB, u.Platform); err != nil {
		return "", 0, err
	} else if ok && multiplier > cap {
		multiplier = cap
	}
	return grade, round4(multiplier), nil
}

func policyDefaultGrade(ctx context.Context, db DB, platform, authType string) (string, error) {
	var g string
	err := db.QueryRow(ctx, `SELECT default_grade FROM detection_policies WHERE platform = $1 AND auth_type IN ($2, '')
		ORDER BY (auth_type = $2) DESC LIMIT 1`, platform, authType).Scan(&g)
	if errors.Is(err, pgx.ErrNoRows) {
		return "B", nil
	}
	return g, err
}

type CreateListingInput struct {
	Mode            string   `json:"mode"`
	Multiplier      *float64 `json:"multiplier"`
	JoinDefaultPool *bool    `json:"join_default_pool"`
	FaceValue       *float64 `json:"face_value"`
	Stock           *int     `json:"stock"`
	Title           *string  `json:"title"`
	Description     *string  `json:"description"`
	MinAmount       *float64 `json:"min_amount"`
	Categories      []string `json:"categories"`
}

// CreateListing 创建挂单。寄售直接生效；回收先生成报价，卖家接受后生效。
func (s *Service) CreateListing(ctx context.Context, sellerID, upstreamID int64, in CreateListingInput) (*Listing, *Quote, error) {
	release, err := s.lockAccountModels(ctx, upstreamID)
	if err != nil {
		return nil, nil, err
	}
	defer release()
	u, err := GetSellerUpstream(ctx, s.DB, sellerID, upstreamID)
	if err != nil {
		return nil, nil, err
	}
	if u.IsReference {
		return nil, nil, httpx.BadRequest("REFERENCE_ACCOUNT", "reference accounts cannot be listed")
	}
	if u.Status != StatusReady || u.Sub2APIAccountID == nil {
		return nil, nil, httpx.Conflict("UPSTREAM_NOT_READY", "upstream must have valid authorization and be ready before listing")
	}
	if privacyPlatforms[u.Platform] && u.AuthType == "oauth" && (u.PrivacyOK == nil || !*u.PrivacyOK) {
		return nil, nil, httpx.Conflict("PRIVACY_REQUIRED", "training data usage must be disabled before listing")
	}
	v, err := GetVendor(ctx, s.DB, u.Platform)
	if err != nil {
		return nil, nil, err
	}
	if !v.Enabled {
		return nil, nil, ErrVendorDisabled
	}
	if u.Health == HealthDegraded || u.Health == HealthUnavailable {
		return nil, nil, httpx.Conflict("UPSTREAM_UNHEALTHY", "unhealthy upstreams cannot be listed")
	}
	if u.Health == HealthUnknown && len(u.ModelWhitelist) == 0 {
		return nil, nil, httpx.Conflict("MODELS_REQUIRED", "select sale models before listing")
	}
	// 回收报价依赖检测证据，不把未检测默认等级当作通过。
	if in.Mode == ModeBuyback && u.LastCheckedAt == nil {
		return nil, nil, httpx.Conflict("INSPECTION_REQUIRED", "platform buyback requires inspection evidence")
	}
	if err := s.requireAuthorizedAccount(ctx, u); err != nil {
		return nil, nil, err
	}
	join := true
	if in.JoinDefaultPool != nil {
		join = *in.JoinDefaultPool
	}
	switch in.Mode {
	case ModeConsignment:
		if in.Multiplier == nil {
			return nil, nil, httpx.BadRequest("MULTIPLIER_REQUIRED", "consignment listing needs a multiplier")
		}
		if err := s.validateConsignment(ctx, u.Platform, *in.Multiplier); err != nil {
			return nil, nil, err
		}
		allowed, err := sellerPickableSet(ctx, s.DB)
		if err != nil {
			return nil, nil, err
		}
		cats, err := NormalizeCategories(ModeConsignment, in.Categories, allowed)
		if err != nil {
			return nil, nil, err
		}
		if err := requireSellerCategory(cats); err != nil {
			return nil, nil, err
		}
		st, err := s.Settings(ctx)
		if err != nil {
			return nil, nil, err
		}
		m := round4(*in.Multiplier)
		tx, err := s.DB.Begin(ctx)
		if err != nil {
			return nil, nil, err
		}
		defer func() { _ = tx.Rollback(ctx) }()
		minAmt := 0.0
		if in.MinAmount != nil && *in.MinAmount > 0 {
			minAmt = round2(*in.MinAmount)
		}
		l, err := scanListing(tx.QueryRow(ctx, `INSERT INTO listings (upstream_id, seller_id, mode, status, multiplier, join_default_pool, price_changed_at, sku_kind, min_amount, title, description, categories)
			VALUES ($1, $2, 'consignment', 'active', $3, $4, now(), 'channel', $5, $6, $7, $8) RETURNING `+listingColumns, u.ID, sellerID, m, join, minAmt, in.Title, in.Description, cats))
		if err != nil {
			return nil, nil, mapListingErr(err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO listing_price_versions (listing_id, multiplier, buyer_price, source) VALUES ($1, $2, $3, 'seller')`,
			l.ID, m, BuyerPrice(m, st.CommissionRate)); err != nil {
			return nil, nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE upstreams SET status = 'listed', updated_at = now() WHERE id = $1`, u.ID); err != nil {
			return nil, nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, nil, err
		}
		s.kick(ctx)
		return l, nil, nil

	case ModeBuyback:
		grade, m, err := s.QuoteFor(ctx, u)
		if err != nil {
			return nil, nil, err
		}
		if m <= 0 {
			return nil, nil, httpx.Conflict("NOT_ELIGIBLE", "this account is not eligible for platform buyback at its current grade")
		}
		st, err := s.Settings(ctx)
		if err != nil {
			return nil, nil, err
		}
		tx, err := s.DB.Begin(ctx)
		if err != nil {
			return nil, nil, err
		}
		defer func() { _ = tx.Rollback(ctx) }()
		l, err := scanListing(tx.QueryRow(ctx, `INSERT INTO listings (upstream_id, seller_id, mode, status, join_default_pool)
			VALUES ($1, $2, 'buyback', 'pending_confirm', true) RETURNING `+listingColumns, u.ID, sellerID))
		if err != nil {
			return nil, nil, mapListingErr(err)
		}
		q, err := insertQuote(ctx, tx, u.ID, l.ID, grade, m, s.now().Add(time.Duration(st.QuoteValidityDays)*24*time.Hour))
		if err != nil {
			return nil, nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, nil, err
		}
		return l, q, nil

	case ModePack:
		if err := validatePackInput(in); err != nil {
			return nil, nil, err
		}
		if err := s.validateConsignment(ctx, u.Platform, *in.Multiplier); err != nil {
			return nil, nil, err
		}
		allowed, err := sellerPickableSet(ctx, s.DB)
		if err != nil {
			return nil, nil, err
		}
		cats, err := NormalizeCategories(ModePack, in.Categories, allowed)
		if err != nil {
			return nil, nil, err
		}
		if err := requireSellerCategory(cats); err != nil {
			return nil, nil, err
		}
		st, err := s.Settings(ctx)
		if err != nil {
			return nil, nil, err
		}
		m := round4(*in.Multiplier)
		face := round2(*in.FaceValue)
		tx, err := s.DB.Begin(ctx)
		if err != nil {
			return nil, nil, err
		}
		defer func() { _ = tx.Rollback(ctx) }()
		l, err := scanListing(tx.QueryRow(ctx, `INSERT INTO listings (
				upstream_id, seller_id, mode, status, multiplier, join_default_pool, price_changed_at,
				sku_kind, face_value, stock, stock_sold, min_amount, title, description, categories)
			VALUES ($1, $2, 'pack', 'active', $3, false, now(), 'pack', $4, $5, 0, 0, $6, $7, $8)
			RETURNING `+listingColumns, u.ID, sellerID, m, face, *in.Stock, in.Title, in.Description, cats))
		if err != nil {
			return nil, nil, mapListingErr(err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO listing_price_versions (listing_id, multiplier, buyer_price, source) VALUES ($1, $2, $3, 'seller')`,
			l.ID, m, BuyerPrice(m, st.CommissionRate)); err != nil {
			return nil, nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE upstreams SET status = 'listed', updated_at = now() WHERE id = $1`, u.ID); err != nil {
			return nil, nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, nil, err
		}
		s.kick(ctx)
		return l, nil, nil
	default:
		return nil, nil, httpx.BadRequest("INVALID_MODE", "mode must be consignment, buyback or pack")
	}
}

func mapListingErr(err error) error {
	if err != nil && isUnique(err) {
		return httpx.Conflict("LISTING_EXISTS", "this upstream already has an open listing")
	}
	return err
}

func (s *Service) validateConsignment(ctx context.Context, platform string, m float64) error {
	v, err := GetVendor(ctx, s.DB, platform)
	if err != nil {
		return err
	}
	if m < v.ConsignmentMin || m > v.ConsignmentMax {
		return httpx.BadRequest("MULTIPLIER_OUT_OF_RANGE", fmt.Sprintf("multiplier must be between %.4f and %.4f", v.ConsignmentMin, v.ConsignmentMax))
	}
	return nil
}

func insertQuote(ctx context.Context, db DB, upstreamID, listingID int64, grade string, m float64, validUntil time.Time) (*Quote, error) {
	if _, err := db.Exec(ctx, `UPDATE buyback_quotes SET status = 'superseded', decided_at = now()
		WHERE upstream_id = $1 AND status = 'offered'`, upstreamID); err != nil {
		return nil, err
	}
	var q Quote
	err := db.QueryRow(ctx, `INSERT INTO buyback_quotes (upstream_id, listing_id, grade, multiplier, status, valid_until)
		VALUES ($1, $2, $3, $4, 'offered', $5) RETURNING id, upstream_id, listing_id, grade, multiplier, status, valid_until, created_at, decided_at`,
		upstreamID, listingID, grade, m, validUntil).
		Scan(&q.ID, &q.UpstreamID, &q.ListingID, &q.Grade, &q.Multiplier, &q.Status, &q.ValidUntil, &q.CreatedAt, &q.DecidedAt)
	return &q, err
}

// CurrentQuote 返回上游当前有效的回收报价，没有时返回 nil。
func CurrentQuote(ctx context.Context, db DB, upstreamID int64) (*Quote, error) {
	var q Quote
	err := db.QueryRow(ctx, `SELECT id, upstream_id, listing_id, grade, multiplier, status, valid_until, created_at, decided_at
		FROM buyback_quotes WHERE upstream_id = $1 AND status IN ('offered', 'accepted') ORDER BY id DESC LIMIT 1`, upstreamID).
		Scan(&q.ID, &q.UpstreamID, &q.ListingID, &q.Grade, &q.Multiplier, &q.Status, &q.ValidUntil, &q.CreatedAt, &q.DecidedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &q, err
}

func (s *Service) sellerListing(ctx context.Context, sellerID, listingID int64) (*Listing, error) {
	l, err := GetListing(ctx, s.DB, listingID)
	if err != nil {
		return nil, err
	}
	if l.SellerID != sellerID {
		return nil, ErrListingNotFound
	}
	return l, nil
}

// AcceptQuote 卖家接受回收报价，挂单按报价倍率生效。
func (s *Service) AcceptQuote(ctx context.Context, sellerID, listingID int64) (*Listing, error) {
	l, err := s.sellerListing(ctx, sellerID, listingID)
	if err != nil {
		return nil, err
	}
	if l.Mode != ModeBuyback || l.Status != ListingPendingConfirm {
		return nil, ErrBadState
	}
	q, err := CurrentQuote(ctx, s.DB, l.UpstreamID)
	if err != nil {
		return nil, err
	}
	if q == nil || q.Status != "offered" || s.now().After(q.ValidUntil) {
		return nil, httpx.Conflict("QUOTE_EXPIRED", "quote expired, please request a new one")
	}
	if err := s.applyQuote(ctx, l, q); err != nil {
		return nil, err
	}
	s.kick(ctx)
	return GetListing(ctx, s.DB, l.ID)
}

func (s *Service) applyQuote(ctx context.Context, l *Listing, q *Quote) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `UPDATE buyback_quotes SET status = 'accepted', decided_at = now() WHERE id = $1`, q.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE listings SET status = 'active', paused_by = NULL, multiplier = $2, price_changed_at = now(), updated_at = now()
		WHERE id = $1`, l.ID, q.Multiplier); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO listing_price_versions (listing_id, multiplier, source) VALUES ($1, $2, 'quote')`, l.ID, q.Multiplier); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE upstreams SET status = 'listed', updated_at = now() WHERE id = $1 AND status = 'ready'`, l.UpstreamID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RejectQuote 卖家拒绝回收报价，挂单关闭。
func (s *Service) RejectQuote(ctx context.Context, sellerID, listingID int64) error {
	l, err := s.sellerListing(ctx, sellerID, listingID)
	if err != nil {
		return err
	}
	if l.Mode != ModeBuyback || l.Status != ListingPendingConfirm {
		return ErrBadState
	}
	if _, err := s.DB.Exec(ctx, `UPDATE buyback_quotes SET status = 'rejected', decided_at = now() WHERE upstream_id = $1 AND status = 'offered'`, l.UpstreamID); err != nil {
		return err
	}
	return s.closeListing(ctx, l)
}

// ChangePrice 寄售挂单改价，受冷却时间限制，只影响之后的请求。
func (s *Service) ChangePrice(ctx context.Context, sellerID, listingID int64, multiplier float64) (*Listing, error) {
	l, err := s.sellerListing(ctx, sellerID, listingID)
	if err != nil {
		return nil, err
	}
	if (l.Mode != ModeConsignment && l.Mode != ModePack) || l.Status == ListingClosed {
		return nil, ErrBadState
	}
	u, err := GetUpstream(ctx, s.DB, l.UpstreamID)
	if err != nil {
		return nil, err
	}
	if err := s.validateConsignment(ctx, u.Platform, multiplier); err != nil {
		return nil, err
	}
	st, err := s.Settings(ctx)
	if err != nil {
		return nil, err
	}
	if l.PriceChangedAt != nil && s.now().Sub(*l.PriceChangedAt) < time.Duration(st.PriceChangeCooldownMinutes)*time.Minute {
		return nil, httpx.TooMany("PRICE_COOLDOWN", fmt.Sprintf("price can be changed once every %d minutes", st.PriceChangeCooldownMinutes))
	}
	m := round4(multiplier)
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `UPDATE listings SET multiplier = $2, price_changed_at = now(), updated_at = now() WHERE id = $1`, l.ID, m); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO listing_price_versions (listing_id, multiplier, buyer_price, source) VALUES ($1, $2, $3, 'seller')`,
		l.ID, m, BuyerPrice(m, st.CommissionRate)); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	s.kick(ctx)
	return GetListing(ctx, s.DB, l.ID)
}

// UpdateListingMetaInput 卖家改挂单展示信息与分类（不改价）。
type UpdateListingMetaInput struct {
	Title       *string  `json:"title"`
	Description *string  `json:"description"`
	Categories  []string `json:"categories"`
}

// UpdateListingMeta 更新标题/描述/分类；仅寄售与额度包、未关闭时可改。
func (s *Service) UpdateListingMeta(ctx context.Context, sellerID, listingID int64, in UpdateListingMetaInput) (*Listing, error) {
	l, err := s.sellerListing(ctx, sellerID, listingID)
	if err != nil {
		return nil, err
	}
	if (l.Mode != ModeConsignment && l.Mode != ModePack) || l.Status == ListingClosed {
		return nil, ErrBadState
	}
	allowed, err := sellerPickableSet(ctx, s.DB)
	if err != nil {
		return nil, err
	}
	cats, err := NormalizeCategories(l.Mode, in.Categories, allowed)
	if err != nil {
		return nil, err
	}
	if err := requireSellerCategory(cats); err != nil {
		return nil, err
	}
	title := in.Title
	desc := in.Description
	if title == nil {
		title = l.Title
	}
	if desc == nil {
		desc = l.Description
	}
	// 空串视为清空
	if title != nil && strings.TrimSpace(*title) == "" {
		title = nil
	}
	if desc != nil && strings.TrimSpace(*desc) == "" {
		desc = nil
	}
	if _, err := s.DB.Exec(ctx, `UPDATE listings SET title = $2, description = $3, categories = $4, updated_at = now() WHERE id = $1`,
		l.ID, title, desc, cats); err != nil {
		return nil, err
	}
	return GetListing(ctx, s.DB, l.ID)
}

// SetJoinDefaultPool 寄售卖家加入或退出默认分组。
func (s *Service) SetJoinDefaultPool(ctx context.Context, sellerID, listingID int64, join bool) (*Listing, error) {
	l, err := s.sellerListing(ctx, sellerID, listingID)
	if err != nil {
		return nil, err
	}
	if l.Mode != ModeConsignment || l.Status == ListingClosed {
		return nil, ErrBadState
	}
	if _, err := s.DB.Exec(ctx, `UPDATE listings SET join_default_pool = $2, updated_at = now() WHERE id = $1`, l.ID, join); err != nil {
		return nil, err
	}
	s.kick(ctx)
	return GetListing(ctx, s.DB, l.ID)
}

// PauseListing / ResumeListing：卖家暂停或恢复挂单。
func (s *Service) PauseListing(ctx context.Context, sellerID, listingID int64) (*Listing, error) {
	l, err := s.sellerListing(ctx, sellerID, listingID)
	if err != nil {
		return nil, err
	}
	if l.Status != ListingActive {
		return nil, ErrBadState
	}
	if _, err := s.DB.Exec(ctx, `UPDATE listings SET status = 'paused', paused_by = 'seller', updated_at = now() WHERE id = $1`, l.ID); err != nil {
		return nil, err
	}
	s.kick(ctx)
	return GetListing(ctx, s.DB, l.ID)
}

func (s *Service) ResumeListing(ctx context.Context, sellerID, listingID int64) (*Listing, error) {
	l, err := s.sellerListing(ctx, sellerID, listingID)
	if err != nil {
		return nil, err
	}
	if l.Status != ListingPaused || (l.PausedBy != nil && *l.PausedBy != "seller") {
		return nil, ErrBadState
	}
	if _, err := s.DB.Exec(ctx, `UPDATE listings SET status = 'active', paused_by = NULL, updated_at = now() WHERE id = $1`, l.ID); err != nil {
		return nil, err
	}
	s.kick(ctx)
	return GetListing(ctx, s.DB, l.ID)
}

// CloseListing 卖家关闭挂单，上游回到 ready，可重新挂单。
func (s *Service) CloseListing(ctx context.Context, sellerID, listingID int64) error {
	l, err := s.sellerListing(ctx, sellerID, listingID)
	if err != nil {
		return err
	}
	return s.closeListing(ctx, l)
}

func (s *Service) closeListing(ctx context.Context, l *Listing) error {
	if l.Status == ListingClosed {
		return nil
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `UPDATE listings SET status = 'closed', closed_at = now(), updated_at = now() WHERE id = $1`, l.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE upstreams SET status = 'ready', updated_at = now() WHERE id = $1 AND status = 'listed'`, l.UpstreamID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	s.kick(ctx)
	return nil
}

// RequoteResult 是重新报价的结果。
type RequoteResult struct {
	ListingID     int64
	OldMultiplier float64
	NewMultiplier float64
	AutoAccepted  bool
}

// Requote 为回收挂单重新报价：新报价不低于旧报价时自动续期，否则挂单暂停等待卖家确认。
func (s *Service) Requote(ctx context.Context, l *Listing) (*RequoteResult, error) {
	if l.Mode != ModeBuyback || l.Multiplier == nil || l.Status == ListingClosed {
		return nil, nil
	}
	u, err := GetUpstream(ctx, s.DB, l.UpstreamID)
	if err != nil {
		return nil, err
	}
	grade, m, err := s.QuoteFor(ctx, u)
	if err != nil {
		return nil, err
	}
	st, err := s.Settings(ctx)
	if err != nil {
		return nil, err
	}
	res := &RequoteResult{ListingID: l.ID, OldMultiplier: *l.Multiplier, NewMultiplier: m}
	q, err := insertQuote(ctx, s.DB, u.ID, l.ID, grade, m, s.now().Add(time.Duration(st.QuoteValidityDays)*24*time.Hour))
	if err != nil {
		return nil, err
	}
	if m >= *l.Multiplier && m > 0 {
		if err := s.applyQuote(ctx, l, q); err != nil {
			return nil, err
		}
		res.AutoAccepted = true
	} else {
		if _, err := s.DB.Exec(ctx, `UPDATE listings SET status = 'pending_confirm', updated_at = now() WHERE id = $1`, l.ID); err != nil {
			return nil, err
		}
	}
	s.kick(ctx)
	return res, nil
}

// ListSellerListings 返回卖家的全部挂单（含已关闭）。
func ListSellerListings(ctx context.Context, db DB, sellerID int64) ([]Listing, error) {
	rows, err := db.Query(ctx, "SELECT "+listingColumns+" FROM listings WHERE seller_id = $1 ORDER BY id DESC", sellerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Listing
	for rows.Next() {
		l, err := scanListing(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *l)
	}
	return out, rows.Err()
}

type PriceVersion struct {
	Multiplier    float64   `json:"multiplier"`
	BuyerPrice    *float64  `json:"buyer_price,omitempty"`
	EffectiveFrom time.Time `json:"effective_from"`
	Source        string    `json:"source"`
}

func PriceHistory(ctx context.Context, db DB, listingID int64) ([]PriceVersion, error) {
	rows, err := db.Query(ctx, `SELECT multiplier, buyer_price, effective_from, source FROM listing_price_versions
		WHERE listing_id = $1 ORDER BY effective_from DESC`, listingID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (PriceVersion, error) {
		var p PriceVersion
		err := row.Scan(&p.Multiplier, &p.BuyerPrice, &p.EffectiveFrom, &p.Source)
		return p, err
	})
}
