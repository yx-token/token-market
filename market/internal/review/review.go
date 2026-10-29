// Package review 买家对商家的星评与短评（挂单可选关联）。
package review

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
)

const (
	StatusVisible = "visible"
	StatusHidden  = "hidden"
	StatusDeleted = "deleted"

	maxContentRunes = 200
	followupAfter   = 7 * 24 * time.Hour
)

// 预置快捷标签（P1）。
var allowedTags = map[string]struct{}{
	"稳定": {}, "便宜": {}, "客服好": {}, "延迟低": {},
	"掉线多": {}, "限流多": {}, "说明不清": {},
}

type Review struct {
	ID         int64      `json:"id"`
	BuyerID    int64      `json:"buyer_id"`
	BuyerMask  string     `json:"buyer_mask,omitempty"`
	SellerID   int64      `json:"seller_id"`
	ListingID  *int64     `json:"listing_id,omitempty"`
	ParentID   *int64     `json:"parent_id,omitempty"`
	Rating     *int       `json:"rating,omitempty"`
	Content    string     `json:"content"`
	Tags       []string   `json:"tags"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	HiddenAt   *time.Time `json:"hidden_at,omitempty"`
	HideReason *string    `json:"hide_reason,omitempty"`
	FollowUp   *Review    `json:"follow_up,omitempty"`
}

type SellerStats struct {
	SellerID    int64   `json:"seller_id"`
	AvgRating   float64 `json:"avg_rating"`
	RatingCount int     `json:"rating_count"`
}

type Eligible struct {
	SellerID      int64  `json:"seller_id"`
	SellerEmail   string `json:"seller_email,omitempty"`
	ListingID     *int64 `json:"listing_id,omitempty"`
	CanReview     bool   `json:"can_review"`
	CanFollowUp   bool   `json:"can_follow_up"`
	ExistingID    *int64 `json:"existing_id,omitempty"`
	ExistingStars *int   `json:"existing_rating,omitempty"`
}

type CreateInput struct {
	SellerID  int64    `json:"seller_id"`
	ListingID *int64   `json:"listing_id"`
	Rating    int      `json:"rating"`
	Content   string   `json:"content"`
	Tags      []string `json:"tags"`
}

type FollowUpInput struct {
	Content string   `json:"content"`
	Tags    []string `json:"tags"`
}

type ListFilter struct {
	SellerID  int64
	ListingID int64
	BuyerID   int64
	Status    string
	MinRating int
	MaxRating int
	Q         string
	Limit     int
	Offset    int
}

type Service struct {
	DB *pgxpool.Pool
}

func (s *Service) Create(ctx context.Context, buyerID int64, in CreateInput) (*Review, error) {
	if in.SellerID <= 0 {
		return nil, httpx.BadRequest("INVALID_SELLER", "seller_id required")
	}
	if in.SellerID == buyerID {
		return nil, httpx.BadRequest("INVALID_SELLER", "cannot review yourself")
	}
	if in.Rating < 1 || in.Rating > 5 {
		return nil, httpx.BadRequest("INVALID_RATING", "rating must be 1–5")
	}
	content := strings.TrimSpace(in.Content)
	if n := utf8.RuneCountInString(content); n > maxContentRunes {
		return nil, httpx.BadRequest("CONTENT_TOO_LONG", "content max 200 characters")
	}
	tags, err := normalizeTags(in.Tags)
	if err != nil {
		return nil, err
	}
	ok, err := s.eligibleBuyerSeller(ctx, buyerID, in.SellerID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, httpx.Forbidden("NOT_ELIGIBLE", "review requires prior usage or pack with this seller")
	}
	if in.ListingID != nil && *in.ListingID > 0 {
		var sid int64
		err := s.DB.QueryRow(ctx, `SELECT seller_id FROM listings WHERE id = $1`, *in.ListingID).Scan(&sid)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, httpx.BadRequest("INVALID_LISTING", "listing not found")
		}
		if err != nil {
			return nil, err
		}
		if sid != in.SellerID {
			return nil, httpx.BadRequest("INVALID_LISTING", "listing does not belong to seller")
		}
	} else {
		in.ListingID = nil
	}

	var id int64
	err = s.DB.QueryRow(ctx, `
		INSERT INTO reviews (buyer_id, seller_id, listing_id, rating, content, tags)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id`,
		buyerID, in.SellerID, in.ListingID, in.Rating, content, tags,
	).Scan(&id)
	if err != nil {
		if isUnique(err) {
			return nil, httpx.Conflict("ALREADY_REVIEWED", "you already reviewed this seller")
		}
		return nil, err
	}
	if err := s.recomputeStats(ctx, in.SellerID); err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

func (s *Service) FollowUp(ctx context.Context, buyerID, parentID int64, in FollowUpInput) (*Review, error) {
	content := strings.TrimSpace(in.Content)
	if content == "" {
		return nil, httpx.BadRequest("CONTENT_REQUIRED", "follow-up requires content")
	}
	if n := utf8.RuneCountInString(content); n > maxContentRunes {
		return nil, httpx.BadRequest("CONTENT_TOO_LONG", "content max 200 characters")
	}
	tags, err := normalizeTags(in.Tags)
	if err != nil {
		return nil, err
	}
	var parent Review
	err = s.DB.QueryRow(ctx, `
		SELECT id, buyer_id, seller_id, listing_id, rating, content, COALESCE(tags,'{}'), status, created_at, updated_at
		FROM reviews WHERE id = $1 AND parent_id IS NULL`, parentID,
	).Scan(&parent.ID, &parent.BuyerID, &parent.SellerID, &parent.ListingID, &parent.Rating,
		&parent.Content, &parent.Tags, &parent.Status, &parent.CreatedAt, &parent.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.NotFound("REVIEW_NOT_FOUND", "review not found")
	}
	if err != nil {
		return nil, err
	}
	if parent.BuyerID != buyerID {
		return nil, httpx.Forbidden("FORBIDDEN", "not your review")
	}
	if parent.Status == StatusDeleted {
		return nil, httpx.Conflict("REVIEW_GONE", "review deleted")
	}
	if time.Since(parent.CreatedAt) < followupAfter {
		return nil, httpx.Conflict("FOLLOWUP_TOO_EARLY", "follow-up available 7 days after the main review")
	}
	var id int64
	err = s.DB.QueryRow(ctx, `
		INSERT INTO reviews (buyer_id, seller_id, listing_id, parent_id, content, tags)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		buyerID, parent.SellerID, parent.ListingID, parentID, content, tags,
	).Scan(&id)
	if err != nil {
		if isUnique(err) {
			return nil, httpx.Conflict("ALREADY_FOLLOWED_UP", "follow-up already exists")
		}
		return nil, err
	}
	return s.Get(ctx, id)
}

func (s *Service) Get(ctx context.Context, id int64) (*Review, error) {
	r, err := s.scanOne(ctx, `SELECT r.id, r.buyer_id, COALESCE(u.email,''), r.seller_id, r.listing_id, r.parent_id,
		r.rating, r.content, COALESCE(r.tags,'{}'), r.status, r.created_at, r.updated_at, r.hidden_at, r.hide_reason
		FROM reviews r LEFT JOIN users u ON u.id = r.buyer_id WHERE r.id = $1`, id)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (s *Service) ListPublic(ctx context.Context, sellerID, listingID int64, onlyListing bool, limit, offset int) ([]Review, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	q := `SELECT r.id, r.buyer_id, COALESCE(u.email,''), r.seller_id, r.listing_id, r.parent_id,
		r.rating, r.content, COALESCE(r.tags,'{}'), r.status, r.created_at, r.updated_at, r.hidden_at, r.hide_reason
		FROM reviews r LEFT JOIN users u ON u.id = r.buyer_id
		WHERE r.status = 'visible' AND r.parent_id IS NULL AND r.seller_id = $1`
	args := []any{sellerID}
	if onlyListing && listingID > 0 {
		args = append(args, listingID)
		q += ` AND r.listing_id = $2`
	}
	args = append(args, limit, offset)
	q += ` ORDER BY r.created_at DESC LIMIT $` + itoa(len(args)-1) + ` OFFSET $` + itoa(len(args))
	rows, err := s.scanMany(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		fu, err := s.followUpOf(ctx, rows[i].ID)
		if err != nil {
			return nil, err
		}
		rows[i].FollowUp = fu
	}
	return rows, nil
}

func (s *Service) ListMine(ctx context.Context, buyerID int64) ([]Review, error) {
	rows, err := s.scanMany(ctx, `SELECT r.id, r.buyer_id, COALESCE(u.email,''), r.seller_id, r.listing_id, r.parent_id,
		r.rating, r.content, COALESCE(r.tags,'{}'), r.status, r.created_at, r.updated_at, r.hidden_at, r.hide_reason
		FROM reviews r LEFT JOIN users u ON u.id = r.buyer_id
		WHERE r.buyer_id = $1 AND r.parent_id IS NULL AND r.status <> 'deleted'
		ORDER BY r.created_at DESC`, buyerID)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		fu, err := s.followUpOf(ctx, rows[i].ID)
		if err != nil {
			return nil, err
		}
		rows[i].FollowUp = fu
	}
	return rows, nil
}

func (s *Service) ListForSeller(ctx context.Context, sellerID int64, limit, offset int) ([]Review, error) {
	return s.ListPublic(ctx, sellerID, 0, false, limit, offset)
}

func (s *Service) AdminList(ctx context.Context, f ListFilter) ([]Review, int, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	where := []string{"r.parent_id IS NULL"}
	args := make([]any, 0, 8)
	push := func(sqlFrag string, v any) {
		args = append(args, v)
		where = append(where, sqlFrag+"$"+itoa(len(args)))
	}
	if f.SellerID > 0 {
		push("r.seller_id = ", f.SellerID)
	}
	if f.BuyerID > 0 {
		push("r.buyer_id = ", f.BuyerID)
	}
	if f.ListingID > 0 {
		push("r.listing_id = ", f.ListingID)
	}
	if f.Status != "" {
		push("r.status = ", f.Status)
	} else {
		where = append(where, "r.status <> 'deleted'")
	}
	if f.MinRating > 0 {
		push("r.rating >= ", f.MinRating)
	}
	if f.MaxRating > 0 {
		push("r.rating <= ", f.MaxRating)
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		args = append(args, "%"+strings.ToLower(q)+"%")
		p := "$" + itoa(len(args))
		where = append(where, "(lower(r.content) LIKE "+p+" OR lower(COALESCE(bu.email,'')) LIKE "+p+" OR lower(COALESCE(su.email,'')) LIKE "+p+")")
	}
	w := strings.Join(where, " AND ")
	var total int
	countQ := `SELECT count(*) FROM reviews r
		LEFT JOIN users bu ON bu.id = r.buyer_id
		LEFT JOIN users su ON su.id = r.seller_id WHERE ` + w
	if err := s.DB.QueryRow(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, f.Limit, f.Offset)
	listQ := `SELECT r.id, r.buyer_id, COALESCE(bu.email,''), r.seller_id, r.listing_id, r.parent_id,
		r.rating, r.content, COALESCE(r.tags,'{}'), r.status, r.created_at, r.updated_at, r.hidden_at, r.hide_reason
		FROM reviews r
		LEFT JOIN users bu ON bu.id = r.buyer_id
		LEFT JOIN users su ON su.id = r.seller_id
		WHERE ` + w + ` ORDER BY r.created_at DESC LIMIT $` + itoa(len(args)-1) + ` OFFSET $` + itoa(len(args))
	rows, err := s.scanMany(ctx, listQ, args...)
	return rows, total, err
}

func (s *Service) Hide(ctx context.Context, id, adminID int64, reason string) error {
	tag, err := s.DB.Exec(ctx, `
		UPDATE reviews SET status = 'hidden', hidden_at = now(), hidden_by = $2, hide_reason = $3, updated_at = now()
		WHERE id = $1 AND parent_id IS NULL AND status = 'visible'`, id, adminID, strings.TrimSpace(reason))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.NotFound("REVIEW_NOT_FOUND", "review not found or not visible")
	}
	var sellerID int64
	_ = s.DB.QueryRow(ctx, `SELECT seller_id FROM reviews WHERE id = $1`, id).Scan(&sellerID)
	return s.recomputeStats(ctx, sellerID)
}

func (s *Service) Unhide(ctx context.Context, id int64) error {
	tag, err := s.DB.Exec(ctx, `
		UPDATE reviews SET status = 'visible', hidden_at = NULL, hidden_by = NULL, hide_reason = NULL, updated_at = now()
		WHERE id = $1 AND parent_id IS NULL AND status = 'hidden'`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.NotFound("REVIEW_NOT_FOUND", "review not found or not hidden")
	}
	var sellerID int64
	_ = s.DB.QueryRow(ctx, `SELECT seller_id FROM reviews WHERE id = $1`, id).Scan(&sellerID)
	return s.recomputeStats(ctx, sellerID)
}

func (s *Service) Delete(ctx context.Context, id, adminID int64, reason string) error {
	tag, err := s.DB.Exec(ctx, `
		UPDATE reviews SET status = 'deleted', hidden_at = now(), hidden_by = $2, hide_reason = $3, updated_at = now()
		WHERE id = $1 AND parent_id IS NULL AND status <> 'deleted'`, id, adminID, strings.TrimSpace(reason))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.NotFound("REVIEW_NOT_FOUND", "review not found")
	}
	_, _ = s.DB.Exec(ctx, `
		UPDATE reviews SET status = 'deleted', updated_at = now()
		WHERE parent_id = $1 AND status <> 'deleted'`, id)
	var sellerID int64
	_ = s.DB.QueryRow(ctx, `SELECT seller_id FROM reviews WHERE id = $1`, id).Scan(&sellerID)
	return s.recomputeStats(ctx, sellerID)
}

func (s *Service) Stats(ctx context.Context, sellerID int64) (SellerStats, error) {
	var st SellerStats
	st.SellerID = sellerID
	err := s.DB.QueryRow(ctx, `
		SELECT COALESCE(avg_rating,0)::float8, COALESCE(rating_count,0)
		FROM seller_rating_stats WHERE seller_id = $1`, sellerID,
	).Scan(&st.AvgRating, &st.RatingCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return st, nil
	}
	return st, err
}

func (s *Service) StatsMap(ctx context.Context, sellerIDs []int64) (map[int64]SellerStats, error) {
	out := make(map[int64]SellerStats, len(sellerIDs))
	if len(sellerIDs) == 0 {
		return out, nil
	}
	rows, err := s.DB.Query(ctx, `
		SELECT seller_id, avg_rating::float8, rating_count FROM seller_rating_stats
		WHERE seller_id = ANY($1)`, sellerIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var st SellerStats
		if err := rows.Scan(&st.SellerID, &st.AvgRating, &st.RatingCount); err != nil {
			return nil, err
		}
		out[st.SellerID] = st
	}
	return out, rows.Err()
}

func (s *Service) Eligible(ctx context.Context, buyerID, sellerID int64) (*Eligible, error) {
	out := &Eligible{SellerID: sellerID}
	_ = s.DB.QueryRow(ctx, `SELECT email FROM users WHERE id = $1`, sellerID).Scan(&out.SellerEmail)
	ok, err := s.eligibleBuyerSeller(ctx, buyerID, sellerID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return out, nil
	}
	var id int64
	var rating *int
	var created time.Time
	err = s.DB.QueryRow(ctx, `
		SELECT id, rating, created_at FROM reviews
		WHERE buyer_id = $1 AND seller_id = $2 AND parent_id IS NULL AND status <> 'deleted'`,
		buyerID, sellerID,
	).Scan(&id, &rating, &created)
	if errors.Is(err, pgx.ErrNoRows) {
		out.CanReview = true
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	out.ExistingID = &id
	out.ExistingStars = rating
	var hasFU bool
	_ = s.DB.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM reviews WHERE parent_id = $1 AND status <> 'deleted')`, id).Scan(&hasFU)
	out.CanFollowUp = !hasFU && time.Since(created) >= followupAfter
	return out, nil
}

func (s *Service) EligibleList(ctx context.Context, buyerID int64) ([]Eligible, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT DISTINCT seller_id FROM (
			SELECT seller_id FROM usage_records
			WHERE buyer_user_id = $1 AND seller_id IS NOT NULL AND channel IN ('direct','pool')
			UNION
			SELECT l.seller_id FROM quota_packs p
			JOIN listings l ON l.id = p.listing_id
			WHERE p.buyer_id = $1 AND p.status IN ('active','exhausted','refunded')
		) t ORDER BY seller_id`, buyerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Eligible
	for rows.Next() {
		var sid int64
		if err := rows.Scan(&sid); err != nil {
			return nil, err
		}
		e, err := s.Eligible(ctx, buyerID, sid)
		if err != nil {
			return nil, err
		}
		if e.CanReview || e.CanFollowUp || e.ExistingID != nil {
			out = append(out, *e)
		}
	}
	return out, rows.Err()
}

func (s *Service) eligibleBuyerSeller(ctx context.Context, buyerID, sellerID int64) (bool, error) {
	var ok bool
	err := s.DB.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM usage_records
			WHERE buyer_user_id = $1 AND seller_id = $2 AND channel IN ('direct','pool')
			UNION ALL
			SELECT 1 FROM quota_packs p
			JOIN listings l ON l.id = p.listing_id
			WHERE p.buyer_id = $1 AND l.seller_id = $2 AND p.status IN ('active','exhausted','refunded')
			LIMIT 1
		)`, buyerID, sellerID).Scan(&ok)
	return ok, err
}

func (s *Service) recomputeStats(ctx context.Context, sellerID int64) error {
	if sellerID <= 0 {
		return nil
	}
	_, err := s.DB.Exec(ctx, `
		INSERT INTO seller_rating_stats (seller_id, avg_rating, rating_count, updated_at)
		SELECT $1,
			COALESCE(round(avg(rating)::numeric, 2), 0),
			COUNT(*),
			now()
		FROM reviews
		WHERE seller_id = $1 AND parent_id IS NULL AND status = 'visible' AND rating IS NOT NULL
		ON CONFLICT (seller_id) DO UPDATE SET
			avg_rating = EXCLUDED.avg_rating,
			rating_count = EXCLUDED.rating_count,
			updated_at = now()`, sellerID)
	return err
}

func (s *Service) followUpOf(ctx context.Context, parentID int64) (*Review, error) {
	r, err := s.scanOne(ctx, `SELECT r.id, r.buyer_id, COALESCE(u.email,''), r.seller_id, r.listing_id, r.parent_id,
		r.rating, r.content, COALESCE(r.tags,'{}'), r.status, r.created_at, r.updated_at, r.hidden_at, r.hide_reason
		FROM reviews r LEFT JOIN users u ON u.id = r.buyer_id
		WHERE r.parent_id = $1 AND r.status = 'visible'`, parentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return r, err
}

func (s *Service) scanOne(ctx context.Context, q string, args ...any) (*Review, error) {
	var r Review
	var email string
	err := s.DB.QueryRow(ctx, q, args...).Scan(
		&r.ID, &r.BuyerID, &email, &r.SellerID, &r.ListingID, &r.ParentID,
		&r.Rating, &r.Content, &r.Tags, &r.Status, &r.CreatedAt, &r.UpdatedAt, &r.HiddenAt, &r.HideReason,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	if r.Tags == nil {
		r.Tags = []string{}
	}
	r.BuyerMask = maskEmail(email)
	return &r, nil
}

func (s *Service) scanMany(ctx context.Context, q string, args ...any) ([]Review, error) {
	rows, err := s.DB.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Review, 0)
	for rows.Next() {
		var r Review
		var email string
		if err := rows.Scan(
			&r.ID, &r.BuyerID, &email, &r.SellerID, &r.ListingID, &r.ParentID,
			&r.Rating, &r.Content, &r.Tags, &r.Status, &r.CreatedAt, &r.UpdatedAt, &r.HiddenAt, &r.HideReason,
		); err != nil {
			return nil, err
		}
		if r.Tags == nil {
			r.Tags = []string{}
		}
		r.BuyerMask = maskEmail(email)
		out = append(out, r)
	}
	return out, rows.Err()
}

func normalizeTags(tags []string) ([]string, error) {
	if len(tags) == 0 {
		return []string{}, nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if _, ok := allowedTags[t]; !ok {
			return nil, httpx.BadRequest("INVALID_TAG", "unknown tag: "+t)
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
		if len(out) > 5 {
			return nil, httpx.BadRequest("TOO_MANY_TAGS", "at most 5 tags")
		}
	}
	return out, nil
}

func maskEmail(email string) string {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" {
		return "买家"
	}
	at := strings.IndexByte(email, '@')
	if at <= 0 {
		return "买家"
	}
	local := email[:at]
	domain := email[at:]
	if len(local) == 1 {
		return local + "***" + domain
	}
	return local[:1] + "***" + domain
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "reviews_one_")
}

func itoa(i int) string { return strconv.Itoa(i) }
