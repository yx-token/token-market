package hostreg

import "testing"

func TestDecide(t *testing.T) {
	wl := []string{"@qq.com", "@gmail.com", "*.edu.cn"}
	cases := []struct {
		email string
		quota bool
		want  Decision
	}{
		{"a@qq.com", false, Allow},
		{"a@gmail.com", false, Allow},
		{"s@cs.edu.cn", false, Allow},
		{"s@edu.cn", false, Allow},
		{"a@dbwot.com", false, Deny},
		{"a@snow.edu.kg", false, Deny},
		{"a@dbwot.com", true, Quota},
		{"a@snow.edu.kg", true, Quota},
		{"a@any.com", false, Allow}, // empty whitelist below
	}
	for _, c := range cases {
		p := Policy{Whitelist: wl, DomainQuotaEnabled: c.quota}
		if c.email == "a@any.com" {
			p.Whitelist = nil
		}
		if got := Decide(c.email, p); got != c.want {
			t.Fatalf("%s quota=%v: got %v want %v", c.email, c.quota, got, c.want)
		}
	}
}

func TestRegistrableDomain(t *testing.T) {
	if got := RegistrableDomain("user@abcd.abc.com"); got != "abc.com" {
		t.Fatalf("got %q", got)
	}
	if got := RegistrableDomain("user@team.example.co.uk"); got != "example.co.uk" {
		t.Fatalf("got %q", got)
	}
}
