// Package promo records promotion visits and immutable signup attribution.
package promo

import (
	"context"
	"crypto/rand"
	"errors"
	"math/big"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
)

type Service struct{ DB *pgxpool.Pool }

var (
	codePattern    = regexp.MustCompile(`^[a-z0-9_-]{2,32}$`)
	visitorPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)
	ErrNotFound    = httpx.NotFound("PROMO_CHANNEL_NOT_FOUND", "promotion channel not found")
	ErrCodeTaken   = httpx.Conflict("PROMO_CODE_TAKEN", "promotion code already exists")
	ErrHasData     = httpx.Conflict("PROMO_CHANNEL_HAS_DATA", "promotion channel already has visits or users")
)

type Channel struct {
	ID          int64     `json:"id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	LandingPath string    `json:"landing_path"`
	Note        string    `json:"note"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Stats       Stats     `json:"stats"`
}

type Input struct {
	Name        string `json:"name"`
	Code        string `json:"code"`
	LandingPath string `json:"landing_path"`
	Note        string `json:"note"`
}

func ValidCode(code string) bool  { return codePattern.MatchString(code) }
func ValidVisitor(id string) bool { return visitorPattern.MatchString(id) }

func validPath(path string) bool {
	decoded, err := url.PathUnescape(path)
	if err != nil {
		return false
	}
	for _, p := range []string{path, decoded} {
		if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || strings.Contains(p, `\`) || strings.Contains(strings.ToLower(p), "http:") || strings.Contains(strings.ToLower(p), "https:") || strings.ContainsFunc(p, unicode.IsControl) {
			return false
		}
	}
	return utf8.RuneCountInString(path) <= 512
}

func (in *Input) validate() error {
	in.Name = strings.TrimSpace(in.Name)
	in.Code = strings.ToLower(strings.TrimSpace(in.Code))
	if in.LandingPath == "" {
		in.LandingPath = "/"
	}
	if in.Name == "" || utf8.RuneCountInString(in.Name) > 64 || utf8.RuneCountInString(in.Note) > 500 || !validPath(in.LandingPath) || (in.Code != "" && !ValidCode(in.Code)) {
		return httpx.BadRequest("INVALID_PROMO_CHANNEL", "invalid name, code, landing_path or note")
	}
	return nil
}

const channelColumns = `id, code, name, landing_path, note, status, created_at, updated_at`

func scanChannel(row pgx.Row) (*Channel, error) {
	c := &Channel{Stats: zeroStats()}
	err := row.Scan(&c.ID, &c.Code, &c.Name, &c.LandingPath, &c.Note, &c.Status, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return c, err
}

func randomCode() (string, error) {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	var out [6]byte
	for i := range out {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", err
		}
		out[i] = alphabet[n.Int64()]
	}
	return string(out[:]), nil
}

func (s *Service) Create(ctx context.Context, in Input, actor int64) (*Channel, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	auto := in.Code == ""
	for range 5 {
		if auto {
			var err error
			in.Code, err = randomCode()
			if err != nil {
				return nil, err
			}
		}
		c, err := scanChannel(s.DB.QueryRow(ctx, `INSERT INTO promo_channels (code,name,landing_path,note,created_by) VALUES ($1,$2,$3,$4,$5) RETURNING `+channelColumns, in.Code, in.Name, in.LandingPath, in.Note, actor))
		var pe *pgconn.PgError
		if errors.As(err, &pe) && pe.Code == "23505" {
			if auto {
				continue
			}
			return nil, ErrCodeTaken
		}
		return c, err
	}
	return nil, ErrCodeTaken
}

func (s *Service) Update(ctx context.Context, id int64, in Input) (*Channel, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	c, err := scanChannel(s.DB.QueryRow(ctx, `UPDATE promo_channels SET name=$2,landing_path=$3,note=$4,updated_at=now() WHERE id=$1 RETURNING `+channelColumns, id, in.Name, in.LandingPath, in.Note))
	if err != nil {
		return nil, err
	}
	return s.withStats(ctx, c)
}

func (s *Service) SetStatus(ctx context.Context, id int64, status string) (*Channel, error) {
	if status != "active" && status != "disabled" {
		return nil, httpx.BadRequest("INVALID_PROMO_STATUS", "invalid status")
	}
	c, err := scanChannel(s.DB.QueryRow(ctx, `UPDATE promo_channels SET status=$2,updated_at=now() WHERE id=$1 RETURNING `+channelColumns, id, status))
	if err != nil {
		return nil, err
	}
	return s.withStats(ctx, c)
}

func (s *Service) Delete(ctx context.Context, id int64) error {
	tag, err := s.DB.Exec(ctx, `DELETE FROM promo_channels WHERE id=$1`, id)
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23503" {
		return ErrHasData
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

type Visit struct {
	Code        string `json:"code"`
	VisitorID   string `json:"visitor_id"`
	LandingPath string `json:"landing_path"`
	Referrer    string `json:"referrer"`
}

func truncate(s string) string {
	r := []rune(s)
	if len(r) > 512 {
		r = r[:512]
	}
	return string(r)
}

func (s *Service) RecordVisit(ctx context.Context, in Visit, ip, ua string) (bool, error) {
	if !ValidCode(in.Code) || !ValidVisitor(in.VisitorID) {
		return false, nil
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// A transaction lock closes the NOT EXISTS race across processes. Hash collisions only serialize unrelated visits.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "promo:"+in.Code+":"+in.VisitorID); err != nil {
		return false, err
	}
	tag, err := tx.Exec(ctx, `INSERT INTO promo_channel_visits (channel_id,visitor_id,ip,user_agent,referrer,landing_path)
		SELECT id,$2,$3,$4,$5,$6 FROM promo_channels c WHERE code=$1 AND status='active'
		AND NOT EXISTS (SELECT 1 FROM promo_channel_visits v WHERE v.channel_id=c.id AND v.visitor_id=$2 AND v.created_at > now()-interval '10 minutes')`,
		in.Code, in.VisitorID, truncate(ip), truncate(ua), truncate(in.Referrer), truncate(in.LandingPath))
	if err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (s *Service) Attribute(ctx context.Context, userID int64, code, visitorID, touchedAt string) error {
	if code == "" {
		return nil
	}
	if !ValidCode(code) || (visitorID != "" && !ValidVisitor(visitorID)) {
		return httpx.BadRequest("INVALID_PROMO_ATTRIBUTION", "invalid attribution")
	}
	var touched *time.Time
	if touchedAt != "" {
		t, err := time.Parse(time.RFC3339Nano, touchedAt)
		if err != nil || time.Since(t) > 30*24*time.Hour {
			return httpx.BadRequest("INVALID_PROMO_ATTRIBUTION", "invalid or expired touch time")
		}
		// touched_at 由浏览器生成，用户电脑时钟走快很常见；超前的时间按服务器当前时间记，不能因此丢掉归因。
		if now := time.Now(); t.After(now) {
			t = now
		}
		touched = &t
	}
	_, err := s.DB.Exec(ctx, `INSERT INTO promo_channel_users (user_id,channel_id,visitor_id,touched_at,created_at)
		SELECT u.id,c.id,NULLIF($3,''),$4,u.created_at FROM users u CROSS JOIN promo_channels c
		WHERE u.id=$1 AND c.code=$2 AND c.status='active' ON CONFLICT (user_id) DO NOTHING`, userID, code, visitorID, touched)
	return err
}
