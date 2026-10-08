// Package account 管理平台用户：注册登录、实名认证、冻结。
// 同一个用户既可以是买家也可以是卖家。
package account

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/DueGin/token-market/internal/platform/auth"
	"github.com/DueGin/token-market/internal/platform/crypto"
	"github.com/DueGin/token-market/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type User struct {
	ID          int64      `json:"id"`
	Email       string     `json:"email"`
	IsAdmin     bool       `json:"is_admin"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`
	KYCStatus   string     `json:"kyc_status"`
}

type Service struct {
	db  *pgxpool.Pool
	kr  *crypto.Keyring
	iss *auth.Issuer
}

func NewService(db *pgxpool.Pool, kr *crypto.Keyring, iss *auth.Issuer) *Service {
	return &Service{db: db, kr: kr, iss: iss}
}

var (
	ErrEmailTaken         = httpx.Conflict("EMAIL_TAKEN", "email already registered")
	ErrInvalidCredentials = httpx.Unauthorized("INVALID_CREDENTIALS", "invalid email or password")
	ErrUserFrozen         = httpx.Forbidden("USER_FROZEN", "account is frozen")
)

func normalizeEmail(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return "", httpx.BadRequest("INVALID_EMAIL", "invalid email address")
	}
	return email, nil
}

// Register 创建普通用户。
func (s *Service) Register(ctx context.Context, email, password, ip string) (*User, error) {
	email, err := normalizeEmail(email)
	if err != nil {
		return nil, err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return nil, httpx.BadRequest("WEAK_PASSWORD", err.Error())
	}
	var u User
	err = s.db.QueryRow(ctx, `INSERT INTO users (email, password_hash) VALUES ($1, $2)
		RETURNING id, email, is_admin, status, created_at`, email, hash).
		Scan(&u.ID, &u.Email, &u.IsAdmin, &u.Status, &u.CreatedAt)
	if isUniqueViolation(err) {
		return nil, ErrEmailTaken
	}
	if err != nil {
		return nil, err
	}
	u.KYCStatus = "none"
	s.RecordIP(ctx, u.ID, ip, "register")
	return &u, nil
}

// Login 校验密码并签发令牌。
func (s *Service) Login(ctx context.Context, email, password, ip string) (string, time.Time, *User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var (
		u    User
		hash string
	)
	err := s.db.QueryRow(ctx, `SELECT id, email, is_admin, status, created_at, password_hash FROM users WHERE email = $1`, email).
		Scan(&u.ID, &u.Email, &u.IsAdmin, &u.Status, &u.CreatedAt, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", time.Time{}, nil, ErrInvalidCredentials
	}
	if err != nil {
		return "", time.Time{}, nil, err
	}
	if !auth.CheckPassword(hash, password) {
		return "", time.Time{}, nil, ErrInvalidCredentials
	}
	if u.Status != "active" {
		return "", time.Time{}, nil, ErrUserFrozen
	}
	tok, exp, err := s.iss.Issue(auth.Claims{UserID: u.ID, IsAdmin: u.IsAdmin})
	if err != nil {
		return "", time.Time{}, nil, err
	}
	if _, err := s.db.Exec(ctx, "UPDATE users SET last_login_at = now() WHERE id = $1", u.ID); err != nil {
		return "", time.Time{}, nil, err
	}
	s.RecordIP(ctx, u.ID, ip, "login")
	return tok, exp, &u, nil
}

// Get 返回用户与实名状态。
func (s *Service) Get(ctx context.Context, id int64) (*User, error) {
	var u User
	var kyc *string
	err := s.db.QueryRow(ctx, `SELECT u.id, u.email, u.is_admin, u.status, u.created_at, u.last_login_at, k.status
		FROM users u LEFT JOIN kyc_profiles k ON k.user_id = u.id WHERE u.id = $1`, id).
		Scan(&u.ID, &u.Email, &u.IsAdmin, &u.Status, &u.CreatedAt, &u.LastLoginAt, &kyc)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.NotFound("USER_NOT_FOUND", "user not found")
	}
	if err != nil {
		return nil, err
	}
	u.KYCStatus = "none"
	if kyc != nil {
		u.KYCStatus = *kyc
	}
	return &u, nil
}

// Refresh 为仍然有效的会话签发新令牌（重新读取管理员标记与状态）。
func (s *Service) Refresh(ctx context.Context, userID int64) (string, time.Time, *User, error) {
	u, err := s.Get(ctx, userID)
	if err != nil {
		return "", time.Time{}, nil, err
	}
	if u.Status != "active" {
		return "", time.Time{}, nil, ErrUserFrozen
	}
	tok, exp, err := s.iss.Issue(auth.Claims{UserID: u.ID, IsAdmin: u.IsAdmin})
	if err != nil {
		return "", time.Time{}, nil, err
	}
	return tok, exp, u, nil
}

// CheckActive 供鉴权中间件确认用户未被冻结。
func (s *Service) CheckActive(ctx context.Context, id int64) error {
	var status string
	err := s.db.QueryRow(ctx, "SELECT status FROM users WHERE id = $1", id).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.Unauthorized("UNAUTHORIZED", "user not found")
	}
	if err != nil {
		return err
	}
	if status != "active" {
		return ErrUserFrozen
	}
	return nil
}

// EnsureBootstrapAdmin 在没有任何管理员时创建一个。
func (s *Service) EnsureBootstrapAdmin(ctx context.Context, email, password string) (bool, error) {
	if email == "" || password == "" {
		return false, nil
	}
	var n int
	if err := s.db.QueryRow(ctx, "SELECT count(*) FROM users WHERE is_admin").Scan(&n); err != nil {
		return false, err
	}
	if n > 0 {
		return false, nil
	}
	email, err := normalizeEmail(email)
	if err != nil {
		return false, err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return false, err
	}
	_, err = s.db.Exec(ctx, `INSERT INTO users (email, password_hash, is_admin) VALUES ($1, $2, true)
		ON CONFLICT (email) DO UPDATE SET is_admin = true`, email, hash)
	return err == nil, err
}

// SetStatus 冻结或解冻用户（管理员）。
func (s *Service) SetStatus(ctx context.Context, userID int64, status string) error {
	if status != "active" && status != "frozen" {
		return httpx.BadRequest("INVALID_STATUS", "status must be active or frozen")
	}
	tag, err := s.db.Exec(ctx, "UPDATE users SET status = $2 WHERE id = $1", userID, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.NotFound("USER_NOT_FOUND", "user not found")
	}
	return nil
}

// RecordIP 记录用户 IP 事件，失败不影响主流程。
func (s *Service) RecordIP(ctx context.Context, userID int64, ip, kind string) {
	if ip == "" {
		return
	}
	_, _ = s.db.Exec(ctx, "INSERT INTO user_ip_events (user_id, ip, kind) VALUES ($1, $2, $3)", userID, ip, kind)
}

// SubmitKYC 提交实名信息。已通过的不能再改。
func (s *Service) SubmitKYC(ctx context.Context, userID int64, realName, idNumber string) error {
	realName = strings.TrimSpace(realName)
	idNumber = strings.ToUpper(strings.TrimSpace(idNumber))
	if realName == "" || len(idNumber) < 6 {
		return httpx.BadRequest("INVALID_KYC", "real name and id number are required")
	}
	aad := fmt.Sprintf("kyc:%d", userID)
	nameEnc, err := s.kr.SealString(realName, aad+":name")
	if err != nil {
		return err
	}
	idEnc, err := s.kr.SealString(idNumber, aad+":id")
	if err != nil {
		return err
	}
	idHash := s.kr.BlindIndex("kyc-id:" + idNumber)
	tag, err := s.db.Exec(ctx, `INSERT INTO kyc_profiles (user_id, real_name_enc, id_number_enc, id_number_hash, status)
		VALUES ($1, $2, $3, $4, 'pending')
		ON CONFLICT (user_id) DO UPDATE SET real_name_enc = EXCLUDED.real_name_enc, id_number_enc = EXCLUDED.id_number_enc,
			id_number_hash = EXCLUDED.id_number_hash, status = 'pending', submitted_at = now(), reviewed_at = NULL, note = NULL
		WHERE kyc_profiles.status <> 'verified'`, userID, nameEnc, idEnc, idHash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.Conflict("KYC_ALREADY_VERIFIED", "kyc already verified")
	}
	return nil
}

// ReviewKYC 管理员审核实名。同一证件号已被其他已认证用户使用时拒绝通过。
func (s *Service) ReviewKYC(ctx context.Context, adminID, userID int64, approve bool, note string) error {
	status := "rejected"
	if approve {
		status = "verified"
		var dup int
		if err := s.db.QueryRow(ctx, `SELECT count(*) FROM kyc_profiles k
			WHERE k.status = 'verified' AND k.user_id <> $1
			  AND k.id_number_hash = (SELECT id_number_hash FROM kyc_profiles WHERE user_id = $1)`, userID).Scan(&dup); err != nil {
			return err
		}
		if dup > 0 {
			return httpx.Conflict("KYC_DUPLICATE_ID", "id number already verified by another user")
		}
	}
	tag, err := s.db.Exec(ctx, `UPDATE kyc_profiles SET status = $2, reviewed_at = now(), reviewed_by = $3, note = $4
		WHERE user_id = $1 AND status = 'pending'`, userID, status, adminID, note)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.NotFound("KYC_NOT_PENDING", "no pending kyc for this user")
	}
	return nil
}

// KYCVerified 判断用户是否已通过实名。
func (s *Service) KYCVerified(ctx context.Context, userID int64) (bool, error) {
	var status string
	err := s.db.QueryRow(ctx, "SELECT status FROM kyc_profiles WHERE user_id = $1", userID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return status == "verified", err
}

type KYCItem struct {
	UserID      int64     `json:"user_id"`
	Email       string    `json:"email"`
	RealName    string    `json:"real_name"`
	IDNumber    string    `json:"id_number_masked"`
	Status      string    `json:"status"`
	SubmittedAt time.Time `json:"submitted_at"`
}

// PendingKYC 列出待审核的实名（证件号脱敏）。
func (s *Service) PendingKYC(ctx context.Context) ([]KYCItem, error) {
	rows, err := s.db.Query(ctx, `SELECT k.user_id, u.email, k.real_name_enc, k.id_number_enc, k.status, k.submitted_at
		FROM kyc_profiles k JOIN users u ON u.id = k.user_id WHERE k.status = 'pending' ORDER BY k.submitted_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []KYCItem
	for rows.Next() {
		var it KYCItem
		var nameEnc, idEnc []byte
		if err := rows.Scan(&it.UserID, &it.Email, &nameEnc, &idEnc, &it.Status, &it.SubmittedAt); err != nil {
			return nil, err
		}
		aad := fmt.Sprintf("kyc:%d", it.UserID)
		if it.RealName, err = s.kr.OpenString(nameEnc, aad+":name"); err != nil {
			return nil, err
		}
		id, err := s.kr.OpenString(idEnc, aad+":id")
		if err != nil {
			return nil, err
		}
		it.IDNumber = mask(id)
		out = append(out, it)
	}
	return out, rows.Err()
}

func mask(s string) string {
	if len(s) <= 6 {
		return "***"
	}
	return s[:3] + strings.Repeat("*", len(s)-6) + s[len(s)-3:]
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
