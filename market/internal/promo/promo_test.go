package promo

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/yx-token/token-market/market/internal/platform/httpx"
)

func assertHTTPError(t *testing.T, err error, status int, code string) {
	t.Helper()
	var business *httpx.Error
	if !errors.As(err, &business) || business.Status != status || business.Code != code {
		t.Fatalf("error = %v, want HTTP %d %s", err, status, code)
	}
}

func TestCodeAndVisitorValidation(t *testing.T) {
	for _, tc := range []struct {
		value string
		valid bool
	}{
		{"ab", true}, {"v2ex-post_1", true}, {strings.Repeat("a", 32), true},
		{"", false}, {"a", false}, {"UPPER", false}, {"bad.code", false},
		{strings.Repeat("a", 33), false}, {"ab\n", false},
	} {
		if got := ValidCode(tc.value); got != tc.valid {
			t.Errorf("ValidCode(%q) = %v, want %v", tc.value, got, tc.valid)
		}
	}
	for _, tc := range []struct {
		value string
		valid bool
	}{
		{"Ab12_-xy", true}, {strings.Repeat("A", 64), true},
		{"", false}, {"short", false}, {strings.Repeat("a", 65), false},
		{"visitor.id", false}, {"visitor id", false}, {"visitor\n1", false},
	} {
		if got := ValidVisitor(tc.value); got != tc.valid {
			t.Errorf("ValidVisitor(%q) = %v, want %v", tc.value, got, tc.valid)
		}
	}
}

func TestInputValidation(t *testing.T) {
	in := Input{Name: "  campaign  ", Code: " V2EX-POST "}
	if err := in.validate(); err != nil {
		t.Fatal(err)
	}
	if in.Name != "campaign" || in.Code != "v2ex-post" || in.LandingPath != "/" {
		t.Fatalf("normalization = %+v", in)
	}
	for _, path := range []string{"/", "/market?kind=api&sort=price", "/%E5%B8%82%E5%9C%BA", "/" + strings.Repeat("a", 511)} {
		if !validPath(path) {
			t.Errorf("valid landing path rejected: %q", path)
		}
	}
	for _, path := range []string{"", "https://outside.test", "//outside.test", "/%2foutside.test", `/\outside.test`, "/%5coutside.test", "/http:outside.test", "/%68%74%74%70%3Aoutside.test", "/\noutside.test", "/%0doutside.test", "/%zz", "/" + strings.Repeat("a", 512)} {
		if validPath(path) {
			t.Errorf("unsafe landing path accepted: %q", path)
		}
	}
	for name, in := range map[string]Input{
		"empty name": {Name: "  "},
		"long name":  {Name: strings.Repeat("a", 65)},
		"long note":  {Name: "campaign", Note: strings.Repeat("a", 501)},
		"bad code":   {Name: "campaign", Code: "bad.code"},
		"bad path":   {Name: "campaign", LandingPath: "//outside.test"},
	} {
		t.Run(name, func(t *testing.T) {
			assertHTTPError(t, in.validate(), http.StatusBadRequest, "INVALID_PROMO_CHANNEL")
		})
	}
	unicodeInput := Input{Name: strings.Repeat("\u754c", 64), Note: strings.Repeat("\u754c", 500)}
	if err := unicodeInput.validate(); err != nil {
		t.Fatalf("length limits must count characters: %v", err)
	}
}

func TestTruncatePreservesCharacters(t *testing.T) {
	for _, input := range []string{"", "short", strings.Repeat("a", 512), strings.Repeat("\u754c", 513)} {
		got := truncate(input)
		wantLength := min(512, utf8.RuneCountInString(input))
		if !utf8.ValidString(got) || utf8.RuneCountInString(got) != wantLength || !strings.HasPrefix(input, got) {
			t.Fatalf("truncate: got %d characters, want %d", utf8.RuneCountInString(got), wantLength)
		}
	}
}

func TestParseRange(t *testing.T) {
	now := time.Date(2026, 11, 4, 17, 30, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, from, to, wantFrom, wantTo string
		daily                            bool
		wantDays                         int
	}{
		{name: "all time"},
		{name: "inclusive beijing days", from: "2026-11-02", to: "2026-11-04", wantFrom: "2026-11-02", wantTo: "2026-11-04", wantDays: 3},
		{name: "from only", from: "2026-11-02", wantFrom: "2026-11-02"},
		{name: "to only", to: "2026-11-04", wantTo: "2026-11-04"},
		{name: "daily default", daily: true, wantFrom: "2026-10-07", wantTo: "2026-11-05", wantDays: 30},
		{name: "daily end only", to: "2026-11-01", daily: true, wantFrom: "2026-10-03", wantTo: "2026-11-01", wantDays: 30},
		{name: "daily start only", from: "2026-11-04", daily: true, wantFrom: "2026-11-04", wantTo: "2026-11-05", wantDays: 2},
		{name: "daily maximum", from: "2024-01-01", to: "2024-12-31", daily: true, wantFrom: "2024-01-01", wantTo: "2024-12-31", wantDays: 366},
		{name: "unlimited list range", from: "2020-01-01", to: "2026-11-05", wantFrom: "2020-01-01", wantTo: "2026-11-05"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseRange(tc.from, tc.to, tc.daily, now)
			if err != nil {
				t.Fatal(err)
			}
			if (got.From == nil) != (tc.wantFrom == "") || (got.To == nil) != (tc.wantTo == "") {
				t.Fatalf("unexpected nullable range: %+v", got)
			}
			if got.From != nil && (*got.From != tc.wantFrom || got.start.In(beijing).Format(time.DateOnly) != tc.wantFrom || got.start.Hour() != 0) {
				t.Fatalf("wrong inclusive start: %+v", got)
			}
			if got.To != nil && (*got.To != tc.wantTo || got.end.Add(-time.Nanosecond).In(beijing).Format(time.DateOnly) != tc.wantTo || got.end.Hour() != 0) {
				t.Fatalf("wrong exclusive end: %+v", got)
			}
			if tc.wantDays != 0 && int(got.end.Sub(*got.start)/(24*time.Hour)) != tc.wantDays {
				t.Fatalf("duration = %v, want %d days", got.end.Sub(*got.start), tc.wantDays)
			}
		})
	}
	for _, tc := range []struct {
		from, to string
		daily    bool
	}{
		{"2026-11-05", "2026-11-04", false}, {"2026-2-01", "", false},
		{"", "2026-02-30", false}, {"2026-11-05T00:00:00Z", "", false},
		{"2024-01-01", "2025-01-01", true},
	} {
		_, err := ParseRange(tc.from, tc.to, tc.daily, now)
		assertHTTPError(t, err, http.StatusBadRequest, "INVALID_DATE_RANGE")
	}
}

func TestStatsAddUsesExactDecimalAmounts(t *testing.T) {
	got := zeroStats()
	got.add(Stats{PV: 2, UV: 1, Signups: 3, PayingUsers: 2, RechargeAmount: "9007199254740992.10", BuyerSpend: "0.10", SellerUsers: 1})
	got.add(Stats{PV: 3, UV: 2, Signups: 1, PayingUsers: 1, RechargeAmount: "0.20", BuyerSpend: "0.20", SellerUsers: 2})
	want := Stats{PV: 5, UV: 3, Signups: 4, PayingUsers: 3, RechargeAmount: "9007199254740992.30", BuyerSpend: "0.30", SellerUsers: 3}
	if got != want {
		t.Fatalf("sum = %+v, want %+v", got, want)
	}
}
