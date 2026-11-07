package account

import (
	"context"
	"errors"
	"strings"

	"github.com/yx-token/token-market/market/hostreg"
)

// enforceRegistrationEmail 套用宿主「邮箱域名白名单 / 非白名单限量」策略。
// 独立进程（无 hostreg）不限制；嵌入宿主时与引擎注册规则一致。
func (s *Service) enforceRegistrationEmail(ctx context.Context, email string) error {
	p, err := hostreg.Load(ctx)
	if errors.Is(err, hostreg.ErrNoSource) {
		return nil
	}
	if err != nil {
		return ErrRegistrationPolicyUnavailable
	}
	switch hostreg.Decide(email, p) {
	case hostreg.Allow:
		return nil
	case hostreg.Deny:
		return ErrEmailSuffixNotAllowed
	case hostreg.Quota:
		domain := hostreg.RegistrableDomain(email)
		if domain == "" {
			return ErrEmailSuffixNotAllowed
		}
		n, err := s.countUsersByEmailDomain(ctx, domain)
		if err != nil {
			return err
		}
		if n > 0 {
			return ErrEmailDomainRegistrationLimit
		}
		return nil
	default:
		return ErrEmailSuffixNotAllowed
	}
}

func (s *Service) countUsersByEmailDomain(ctx context.Context, domain string) (int, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return 0, nil
	}
	escaped := escapeLikeWildcards(domain)
	exact := "%@" + escaped
	sub := "%@%." + escaped
	var n int
	err := s.db.QueryRow(ctx, `
		SELECT COUNT(1) FROM users
		 WHERE RTRIM(LOWER(TRIM(email)), '.') LIKE $1 ESCAPE '\'
		    OR RTRIM(LOWER(TRIM(email)), '.') LIKE $2 ESCAPE '\'`,
		exact, sub,
	).Scan(&n)
	return n, err
}

var likeWildcardEscaper = strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`)

func escapeLikeWildcards(value string) string {
	return likeWildcardEscaper.Replace(value)
}
