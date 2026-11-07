package account

import (
	"context"
	"testing"

	"github.com/yx-token/token-market/market/hostreg"
)

func TestEnforceRegistrationEmail_NoSourceAllows(t *testing.T) {
	hostreg.SetSource(nil)
	t.Cleanup(func() { hostreg.SetSource(nil) })
	s := &Service{}
	if err := s.enforceRegistrationEmail(context.Background(), "a@dbwot.com"); err != nil {
		t.Fatal(err)
	}
}

func TestEnforceRegistrationEmail_DenyNonWhitelist(t *testing.T) {
	hostreg.SetSource(func(context.Context) (hostreg.Policy, error) {
		return hostreg.Policy{
			Whitelist:          []string{"@qq.com", "@gmail.com", "*.edu.cn"},
			DomainQuotaEnabled: false,
		}, nil
	})
	t.Cleanup(func() { hostreg.SetSource(nil) })
	s := &Service{}
	if err := s.enforceRegistrationEmail(context.Background(), "a@dbwot.com"); err != ErrEmailSuffixNotAllowed {
		t.Fatalf("got %v", err)
	}
	if err := s.enforceRegistrationEmail(context.Background(), "a@snow.edu.kg"); err != ErrEmailSuffixNotAllowed {
		t.Fatalf("got %v", err)
	}
	if err := s.enforceRegistrationEmail(context.Background(), "a@qq.com"); err != nil {
		t.Fatal(err)
	}
	if err := s.enforceRegistrationEmail(context.Background(), "s@cs.edu.cn"); err != nil {
		t.Fatal(err)
	}
}

func TestEnforceRegistrationEmail_QuotaNeedsDB(t *testing.T) {
	hostreg.SetSource(func(context.Context) (hostreg.Policy, error) {
		return hostreg.Policy{
			Whitelist:          []string{"@qq.com"},
			DomainQuotaEnabled: true,
		}, nil
	})
	t.Cleanup(func() { hostreg.SetSource(nil) })
	// 无 DB 时 Quota 路径会查库失败；这里只验证白名单仍放行。
	s := &Service{}
	if err := s.enforceRegistrationEmail(context.Background(), "a@qq.com"); err != nil {
		t.Fatal(err)
	}
}

func TestEscapeLikeWildcards(t *testing.T) {
	if got := escapeLikeWildcards(`a_b%c\d`); got != `a\_b\%c\\d` {
		t.Fatalf("got %q", got)
	}
}
