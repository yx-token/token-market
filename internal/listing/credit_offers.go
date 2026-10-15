package listing

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/DueGin/token-market/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
)

// CreditOffer is a seller-wide promotion, independent of any upstream or legacy quota pack.
type CreditOffer struct {
	ID               int64     `json:"id"`
	SellerID         int64     `json:"seller_id"`
	Title            string    `json:"title"`
	Description      string    `json:"description"`
	PriceCNY         float64   `json:"price_cny"`
	FaceValueUSD     float64   `json:"face_value_usd"`
	AllowedPlatforms []string  `json:"allowed_platforms"`
	AllowedModels    []string  `json:"allowed_models"`
	Status           string    `json:"status"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// CreditOfferInput deliberately has no seller ID: ownership always comes from authentication.
type CreditOfferInput struct {
	Title            string   `json:"title"`
	Description      string   `json:"description"`
	PriceCNY         float64  `json:"price_cny"`
	FaceValueUSD     float64  `json:"face_value_usd"`
	AllowedPlatforms []string `json:"allowed_platforms"`
	AllowedModels    []string `json:"allowed_models"`
	Status           string   `json:"status,omitempty"`
}

const creditOfferColumns = `id, seller_id, title, description, price_cny::float8,
	face_value_usd::float8, allowed_platforms, allowed_models, status, created_at, updated_at`

var ErrCreditOfferNotFound = httpx.NotFound("CREDIT_OFFER_NOT_FOUND", "store credit offer not found")

func scanCreditOffer(row pgx.Row) (*CreditOffer, error) {
	var offer CreditOffer
	err := row.Scan(&offer.ID, &offer.SellerID, &offer.Title, &offer.Description, &offer.PriceCNY,
		&offer.FaceValueUSD, &offer.AllowedPlatforms, &offer.AllowedModels, &offer.Status,
		&offer.CreatedAt, &offer.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if offer.AllowedPlatforms == nil {
		offer.AllowedPlatforms = []string{}
	}
	if offer.AllowedModels == nil {
		offer.AllowedModels = []string{}
	}
	return &offer, nil
}

func validCreditOfferMoney(amount float64) bool {
	if math.IsNaN(amount) || math.IsInf(amount, 0) || amount <= 0 || amount > 1e9 {
		return false
	}
	// Check the shortest decimal representation before PostgreSQL can silently round NUMERIC(12,2).
	value := strconv.FormatFloat(amount, 'f', -1, 64)
	point := strings.IndexByte(value, '.')
	return point < 0 || len(value)-point-1 <= 2
}

func normalizeCreditOfferInput(in CreditOfferInput) (CreditOfferInput, error) {
	in.Title = strings.TrimSpace(in.Title)
	in.Description = strings.TrimSpace(in.Description)
	if utf8.RuneCountInString(in.Title) < 1 || utf8.RuneCountInString(in.Title) > 120 || strings.ContainsRune(in.Title, '\x00') {
		return in, httpx.BadRequest("INVALID_CREDIT_TITLE", "title must contain 1 to 120 characters")
	}
	if utf8.RuneCountInString(in.Description) > 4000 || strings.ContainsRune(in.Description, '\x00') {
		return in, httpx.BadRequest("INVALID_CREDIT_DESCRIPTION", "description must contain at most 4000 characters")
	}
	if !validCreditOfferMoney(in.PriceCNY) || !validCreditOfferMoney(in.FaceValueUSD) {
		return in, httpx.BadRequest("INVALID_CREDIT_AMOUNT", "price_cny and face_value_usd must be positive, at most 1000000000, with at most two decimals")
	}
	if in.Status != "" && in.Status != "active" && in.Status != "paused" {
		return in, httpx.BadRequest("INVALID_CREDIT_STATUS", "status must be active or paused")
	}
	var err error
	in.AllowedPlatforms, err = normalizeCreditScope(in.AllowedPlatforms, 64, 80)
	if err != nil {
		return in, err
	}
	in.AllowedModels, err = normalizeCreditScope(in.AllowedModels, 256, 200)
	return in, err
}

func normalizeCreditScope(values []string, maxCount, maxLength int) ([]string, error) {
	if len(values) > maxCount {
		return nil, httpx.BadRequest("INVALID_CREDIT_SCOPE", "too many credit scope entries")
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || utf8.RuneCountInString(value) > maxLength || strings.ContainsAny(value, "*?\r\n\x00") {
			return nil, httpx.BadRequest("INVALID_CREDIT_SCOPE", "credit scope requires non-empty exact platform and model identifiers")
		}
		if !seen[value] {
			out = append(out, value)
			seen[value] = true
		}
	}
	return out, nil
}

func validateCreditOfferPlatforms(ctx context.Context, db DB, platforms []string) error {
	if len(platforms) == 0 {
		return nil
	}
	var count int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM vendor_configs WHERE platform = ANY($1::text[]) AND enabled`, platforms).Scan(&count); err != nil {
		return err
	}
	if count != len(platforms) {
		return httpx.BadRequest("INVALID_CREDIT_PLATFORM", "all allowed_platforms must be enabled vendors")
	}
	return nil
}

func CreateCreditOffer(ctx context.Context, db DB, sellerID int64, in CreditOfferInput) (*CreditOffer, error) {
	if sellerID <= 0 {
		return nil, httpx.BadRequest("INVALID_SELLER", "seller_id must be positive")
	}
	in, err := normalizeCreditOfferInput(in)
	if err != nil {
		return nil, err
	}
	if err := validateCreditOfferPlatforms(ctx, db, in.AllowedPlatforms); err != nil {
		return nil, err
	}
	if in.Status == "" {
		in.Status = "active"
	}
	return scanCreditOffer(db.QueryRow(ctx, `INSERT INTO store_credit_offers
		(seller_id, title, description, price_cny, face_value_usd, allowed_platforms, allowed_models, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING `+creditOfferColumns,
		sellerID, in.Title, in.Description, in.PriceCNY, in.FaceValueUSD, in.AllowedPlatforms, in.AllowedModels, in.Status))
}

// UpdateCreditOffer never changes purchased snapshots or ownership, including under concurrent updates.
// Omitting status preserves the existing status, so editing a paused offer cannot accidentally publish it.
func UpdateCreditOffer(ctx context.Context, db DB, sellerID, id int64, in CreditOfferInput) (*CreditOffer, error) {
	if sellerID <= 0 || id <= 0 {
		return nil, ErrCreditOfferNotFound
	}
	in, err := normalizeCreditOfferInput(in)
	if err != nil {
		return nil, err
	}
	if err := validateCreditOfferPlatforms(ctx, db, in.AllowedPlatforms); err != nil {
		return nil, err
	}
	offer, err := scanCreditOffer(db.QueryRow(ctx, `UPDATE store_credit_offers SET
		title = $3, description = $4, price_cny = $5, face_value_usd = $6,
		allowed_platforms = $7, allowed_models = $8, status = COALESCE(NULLIF($9, ''), status), updated_at = now()
		WHERE seller_id = $1 AND id = $2 RETURNING `+creditOfferColumns,
		sellerID, id, in.Title, in.Description, in.PriceCNY, in.FaceValueUSD, in.AllowedPlatforms, in.AllowedModels, in.Status))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCreditOfferNotFound
	}
	return offer, err
}

func GetCreditOffer(ctx context.Context, db DB, id int64) (*CreditOffer, error) {
	offer, err := scanCreditOffer(db.QueryRow(ctx, `SELECT `+creditOfferColumns+` FROM store_credit_offers WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCreditOfferNotFound
	}
	return offer, err
}

func ListCreditOffers(ctx context.Context, db DB, sellerID int64, activeOnly bool) ([]CreditOffer, error) {
	if sellerID <= 0 {
		return nil, httpx.BadRequest("INVALID_SELLER", "seller_id must be positive")
	}
	rows, err := db.Query(ctx, `SELECT `+creditOfferColumns+` FROM store_credit_offers
		WHERE seller_id = $1 AND (NOT $2::boolean OR status = 'active') ORDER BY id DESC`, sellerID, activeOnly)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]CreditOffer, 0)
	for rows.Next() {
		offer, err := scanCreditOffer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *offer)
	}
	return out, rows.Err()
}
