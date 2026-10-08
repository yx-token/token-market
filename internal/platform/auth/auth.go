// Package auth 提供平台用户的密码哈希与访问令牌。
package auth

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

var ErrInvalidToken = errors.New("auth: invalid token")

// HashPassword 用 bcrypt 生成密码哈希。
func HashPassword(password string) (string, error) {
	if len(password) < 8 {
		return "", errors.New("auth: password must be at least 8 characters")
	}
	if len(password) > 72 {
		return "", errors.New("auth: password must be at most 72 bytes")
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(h), nil
}

// CheckPassword 校验密码。
func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

type Claims struct {
	UserID  int64
	IsAdmin bool
}

type Issuer struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

func NewIssuer(secret string, ttl time.Duration) (*Issuer, error) {
	if len(secret) < 32 {
		return nil, errors.New("auth: jwt secret must be at least 32 bytes")
	}
	return &Issuer{secret: []byte(secret), ttl: ttl, now: time.Now}, nil
}

type jwtClaims struct {
	Admin bool `json:"adm,omitempty"`
	jwt.RegisteredClaims
}

// Issue 签发访问令牌。
func (i *Issuer) Issue(c Claims) (string, time.Time, error) {
	now := i.now()
	exp := now.Add(i.ttl)
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwtClaims{
		Admin: c.IsAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatInt(c.UserID, 10),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
			Issuer:    "token-market",
		},
	})
	s, err := tok.SignedString(i.secret)
	if err != nil {
		return "", time.Time{}, err
	}
	return s, exp, nil
}

// Parse 校验并解析访问令牌。
func (i *Issuer) Parse(token string) (Claims, error) {
	var jc jwtClaims
	parsed, err := jwt.ParseWithClaims(token, &jc, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method %v", t.Header["alg"])
		}
		return i.secret, nil
	}, jwt.WithTimeFunc(i.now), jwt.WithIssuer("token-market"))
	if err != nil || !parsed.Valid {
		return Claims{}, ErrInvalidToken
	}
	id, err := strconv.ParseInt(jc.Subject, 10, 64)
	if err != nil || id <= 0 {
		return Claims{}, ErrInvalidToken
	}
	return Claims{UserID: id, IsAdmin: jc.Admin}, nil
}
