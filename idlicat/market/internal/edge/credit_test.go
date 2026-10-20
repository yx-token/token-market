package edge

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/yx-token/idlicat/market/internal/edgecfg"
)

type creditSelectFunc func(context.Context, *edgecfg.KeyConfig, edgecfg.Hop, string, int64) (*CreditLease, error)

func (f creditSelectFunc) Select(ctx context.Context, cfg *edgecfg.KeyConfig, hop edgecfg.Hop, model string, after int64) (*CreditLease, error) {
	return f(ctx, cfg, hop, model, after)
}

func TestCreditRequestModel(t *testing.T) {
	for _, tc := range []struct{ path, body, want string }{
		{"/v1/messages", `{"model":"claude-sonnet"}`, "claude-sonnet"},
		{"/v1/chat/completions", `{"model":"gpt-6-astra"}`, "gpt-6-astra"},
		{"/v1/responses", `{"model":false}`, ""},
		{"/v1/responses", `{}`, ""},
		{"/v1beta/models/gemini-2.5-pro:generateContent", `{"model":"other"}`, "gemini-2.5-pro"},
		{"/antigravity/v1beta/models/gemini-2.5-pro:streamGenerateContent", `{}`, "gemini-2.5-pro"},
		{"/v1beta/models/gemini:unknown", `{"model":"other"}`, ""},
	} {
		if got := creditRequestModel(tc.path, []byte(tc.body)); got != tc.want {
			t.Errorf("%s %s: got %q want %q", tc.path, tc.body, got, tc.want)
		}
	}
}

func TestCreditInsufficientOnlyAdmissionError(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		want   bool
	}{
		{403, `{"code":"INSUFFICIENT_BALANCE"}`, true},
		{403, `{"error":{"code":"INSUFFICIENT_BALANCE"}}`, true},
		{403, `{"error":{"type":"INSUFFICIENT_BALANCE"}}`, true},
		{402, `{"error":{"code":"INSUFFICIENT_BALANCE"}}`, false},
		{503, `{"code":"INSUFFICIENT_BALANCE"}`, false},
		{403, `{"error":{"message":"INSUFFICIENT_BALANCE"}}`, false},
		{403, `{"error":{"code":"VENDOR_INSUFFICIENT_BALANCE"}}`, false},
		{403, `{"request":{"code":"INSUFFICIENT_BALANCE"}}`, false},
	} {
		if got := creditInsufficient(tc.status, []byte(tc.body)); got != tc.want {
			t.Errorf("%d %s: got %v", tc.status, tc.body, got)
		}
	}
}

func TestCreditBalanceFailureFallsBackToSameSellerWithoutBreaker(t *testing.T) {
	e := newEnv(t)
	released := 0
	e.srv.SetCreditSelector(creditSelectFunc(func(_ context.Context, _ *edgecfg.KeyConfig, _ edgecfg.Hop, _ string, after int64) (*CreditLease, error) {
		if after > 0 {
			return nil, nil
		}
		return &CreditLease{PackID: 1, Key: "credit", Release: func() { released++ }}, nil
	}))
	e.gateway.byKey["credit"] = func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error":{"code":"INSUFFICIENT_BALANCE"}}`)
	}
	e.gateway.byKey["normal"] = ok(`{"paid":"cash"}`)
	e.put(t, "tm-buyer", edgecfg.KeyConfig{BuyerKeyID: 1, BuyerUserID: 1, CreditCandidate: true, Status: "active", Hops: []edgecfg.Hop{e.hop(t, 1, 1, "normal")}})
	for i := 0; i < 3; i++ {
		if rec := e.do(http.MethodPost, "/v1/messages", "tm-buyer", `{"model":"x"}`); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "cash") {
			t.Fatalf("request %d: %d %s", i, rec.Code, rec.Body.String())
		}
	}
	if released != 3 || strings.Join(e.gateway.seenKeys, ",") != "credit,normal,credit,normal,credit,normal" {
		t.Fatalf("fallback/release mismatch: %d %v", released, e.gateway.seenKeys)
	}
}

func TestCreditNonBalanceErrorMustNotReplay(t *testing.T) {
	e := newEnv(t)
	e.srv.SetCreditSelector(creditSelectFunc(func(context.Context, *edgecfg.KeyConfig, edgecfg.Hop, string, int64) (*CreditLease, error) {
		return &CreditLease{PackID: 1, Key: "credit", Release: func() {}}, nil
	}))
	e.gateway.byKey["credit"] = func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error":{"message":"vendor balance insufficient"}}`)
	}
	e.gateway.byKey["normal"] = ok(`{}`)
	e.put(t, "tm-buyer", edgecfg.KeyConfig{BuyerKeyID: 1, BuyerUserID: 1, CreditCandidate: true, Status: "active", Hops: []edgecfg.Hop{e.hop(t, 1, 1, "normal")}})
	rec := e.do(http.MethodPost, "/v1/messages", "tm-buyer", `{"model":"x"}`)
	if rec.Code != 403 || !strings.Contains(rec.Body.String(), "vendor balance insufficient") || len(e.gateway.seenKeys) != 1 {
		t.Fatalf("non-admission 403 must pass through: %d %s %v", rec.Code, rec.Body.String(), e.gateway.seenKeys)
	}
}

func TestCreditBalanceFailurePrefersNextPackBeforeCash(t *testing.T) {
	e := newEnv(t)
	released := 0
	e.srv.SetCreditSelector(creditSelectFunc(func(_ context.Context, _ *edgecfg.KeyConfig, _ edgecfg.Hop, _ string, after int64) (*CreditLease, error) {
		if after == 0 {
			return &CreditLease{PackID: 7, Key: "credit-empty", Release: func() { released++ }}, nil
		}
		if after == 7 {
			return &CreditLease{PackID: 9, Key: "credit-ready", Release: func() { released++ }}, nil
		}
		t.Fatalf("unexpected FIFO cursor: %d", after)
		return nil, nil
	}))
	e.gateway.byKey["credit-empty"] = func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error":{"type":"INSUFFICIENT_BALANCE"}}`)
	}
	e.gateway.byKey["credit-ready"] = ok(`{"paid":"next-credit"}`)
	e.gateway.byKey["normal"] = status(http.StatusForbidden)
	e.put(t, "tm-buyer", edgecfg.KeyConfig{BuyerKeyID: 1, BuyerUserID: 1, CreditCandidate: true, Status: "active", Hops: []edgecfg.Hop{e.hop(t, 1, 1, "normal")}})
	if rec := e.do(http.MethodPost, "/v1/messages", "tm-buyer", `{"model":"x"}`); rec.Code != 200 || !strings.Contains(rec.Body.String(), "next-credit") {
		t.Fatalf("next activity pack should be used: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Join(e.gateway.seenKeys, ",") != "credit-empty,credit-ready" || released != 2 {
		t.Fatalf("cash used before available credit: %v releases=%d", e.gateway.seenKeys, released)
	}
}

func TestCreditDatabaseErrorDoesNotSilentlyChargeCash(t *testing.T) {
	e := newEnv(t)
	e.srv.SetCreditSelector(creditSelectFunc(func(context.Context, *edgecfg.KeyConfig, edgecfg.Hop, string, int64) (*CreditLease, error) {
		return nil, errors.New("database unavailable")
	}))
	e.gateway.byKey["normal"] = ok(`{}`)
	e.put(t, "tm-buyer", edgecfg.KeyConfig{BuyerKeyID: 1, BuyerUserID: 1, CreditCandidate: true, Status: "active", Hops: []edgecfg.Hop{e.hop(t, 1, 1, "normal")}})
	if rec := e.do(http.MethodPost, "/v1/messages", "tm-buyer", `{"model":"x"}`); rec.Code != 503 || len(e.gateway.seenKeys) != 0 {
		t.Fatalf("DB failure must stop before billing: %d %v", rec.Code, e.gateway.seenKeys)
	}
}

func TestCreditDoesNotInterceptFreeEndpoints(t *testing.T) {
	e := newEnv(t)
	e.srv.SetCreditSelector(creditSelectFunc(func(context.Context, *edgecfg.KeyConfig, edgecfg.Hop, string, int64) (*CreditLease, error) {
		t.Fatal("free endpoint must not select credit")
		return nil, nil
	}))
	e.gateway.byKey["normal"] = ok(`{}`)
	e.put(t, "tm-buyer", edgecfg.KeyConfig{BuyerKeyID: 1, BuyerUserID: 1, CreditCandidate: true, Status: "active", Hops: []edgecfg.Hop{e.hop(t, 1, 1, "normal")}})
	for _, path := range []string{"/v1/messages/count_tokens", "/v1beta/models/gemini:countTokens"} {
		if rec := e.do(http.MethodPost, path, "tm-buyer", `{"model":"x"}`); rec.Code != 200 {
			t.Fatalf("%s: %d", path, rec.Code)
		}
	}
}

func TestOrdinaryKeyWithoutCreditCandidateDoesNotQueryCredit(t *testing.T) {
	e := newEnv(t)
	e.srv.SetCreditSelector(creditSelectFunc(func(context.Context, *edgecfg.KeyConfig, edgecfg.Hop, string, int64) (*CreditLease, error) {
		t.Fatal("ordinary key without credit must not acquire DB dependency")
		return nil, nil
	}))
	e.gateway.byKey["normal"] = ok(`{}`)
	e.put(t, "tm-buyer", edgecfg.KeyConfig{BuyerKeyID: 1, BuyerUserID: 1, Status: "active", Hops: []edgecfg.Hop{e.hop(t, 1, 1, "normal")}})
	if rec := e.do(http.MethodPost, "/v1/messages", "tm-buyer", `{"model":"x"}`); rec.Code != 200 {
		t.Fatalf("ordinary request: %d", rec.Code)
	}
}
