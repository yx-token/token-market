package rewardtask

import (
	"context"
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"testing"
	"time"
)

// 向量由 openssl 独立计算：printf '%s\n%s\n%s' ts nonce body | openssl dgst -sha256 -hmac secret
func TestSignMatchesOpenSSLVector(t *testing.T) {
	body := `{"event_id":"evt_1","event_type":"register","ticket":"rt_0123456789abcdef0123456789abcdef01234567","external_user_id":"u_1001"}`
	got := Sign("rs_0123456789abcdef0123456789abcdef0123456789abcdef", "1730790000", "0123456789abcdef0123456789abcdef", []byte(body))
	if want := "6ff77c5578683609b811db894896beb0c798034db1d6381e7e9e7c13b68b8d5a"; got != want {
		t.Fatalf("signature = %s, want %s", got, want)
	}
}

var nonceRE = regexp.MustCompile(`^[A-Za-z0-9_-]{16,64}$`)

// verifyLikeLoxo 复刻 loxo verifyRewardOpenSignature 的检查（backend/internal/service/reward_task_open.go）。
func verifyLikeLoxo(t *testing.T, r *http.Request, secret string, body []byte, now time.Time) {
	t.Helper()
	ts, nonce, sig := r.Header.Get("X-Reward-Timestamp"), r.Header.Get("X-Reward-Nonce"), r.Header.Get("X-Reward-Signature")
	if r.Header.Get("X-Reward-App-Key") == "" || !nonceRE.MatchString(nonce) || len(sig) != 64 {
		t.Fatalf("signature headers malformed: %v", r.Header)
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || sec < now.Unix()-300 || sec > now.Unix()+300 {
		t.Fatalf("timestamp %q outside window", ts)
	}
	want, _ := hex.DecodeString(Sign(secret, ts, nonce, body))
	got, _ := hex.DecodeString(sig)
	if !hmac.Equal(want, got) {
		t.Fatal("signature mismatch")
	}
}

func TestClientSignsAndDecodesEnvelope(t *testing.T) {
	now := time.Unix(1_730_790_000, 0)
	nonces := map[string]bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		verifyLikeLoxo(t, r, "rs_secret_value", body, now)
		if nonces[r.Header.Get("X-Reward-Nonce")] {
			t.Fatal("nonce reused")
		}
		nonces[r.Header.Get("X-Reward-Nonce")] = true
		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("content type %q", r.Header.Get("Content-Type"))
		}
		switch r.URL.Path {
		case "/api/v1/reward-open/ping":
			if string(body) != "{}" {
				t.Fatalf("ping body %s", body)
			}
			_, _ = io.WriteString(w, `{"code":0,"message":"success","data":{"partner_name":"闲猫","app_key":"rk_1","server_time":1730790003}}`)
		case "/api/v1/reward-open/events":
			var in map[string]any
			if err := json.Unmarshal(body, &in); err != nil || in["amount"] != "100.50" || in["order_id"] != "9" {
				t.Fatalf("event body %s", body)
			}
			_, _ = io.WriteString(w, `{"code":0,"message":"success","data":{"event_id":"e","duplicate":false,"result":"completed","task_id":1,"task_type":"recharge","status":"completed","rewarded":true}}`)
		case "/api/v1/reward-open/tickets/query":
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"code":404,"message":"ticket not found","reason":"REWARD_TICKET_NOT_FOUND"}`)
		default:
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, `<html>bad gateway</html>`)
		}
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL + "/", AppKey: "rk_1", Secret: "rs_secret_value", Now: func() time.Time { return now }}
	ctx := context.Background()

	ping, err := c.Ping(ctx)
	if err != nil || ping.PartnerName != "闲猫" || ping.ServerTime != 1730790003 {
		t.Fatalf("ping = %+v, %v", ping, err)
	}
	res, raw, err := c.SendEvent(ctx, EventInput{EventID: "e", EventType: "recharge", Ticket: "rt_x", ExternalUserID: "7", Amount: "100.50", OrderID: "9"})
	if err != nil || res.Result != "completed" || !res.Rewarded || res.Status != "completed" || len(raw) == 0 {
		t.Fatalf("event = %+v, %s, %v", res, raw, err)
	}
	_, err = c.QueryTicket(ctx, "rt_missing_ticket")
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 404 || ae.Reason != "REWARD_TICKET_NOT_FOUND" || reasonOf(err) != "REWARD_TICKET_NOT_FOUND" {
		t.Fatalf("query error = %#v", err)
	}
	_, err = c.call(ctx, "/unknown", []byte("{}"), nil)
	if !errors.As(err, &ae) || ae.Status != http.StatusBadGateway || ae.Reason != "" {
		t.Fatalf("html error = %#v", err)
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		err  error
		want outcome
	}{
		{errors.New("dial tcp: connection refused"), retrySoon},
		{&APIError{Status: 502, Message: "html"}, retrySoon},
		{&APIError{Status: 401, Reason: "REWARD_SIGNATURE_INVALID"}, retrySoon},
		{&APIError{Status: 401, Reason: "REWARD_NONCE_REPLAYED"}, retrySoon},
		{&APIError{Status: 403, Reason: "REWARD_TASKS_DISABLED"}, retrySoon},
		{&APIError{Status: 429, Reason: "RATE_LIMITED"}, retrySoon},
		{&APIError{Status: 409, Reason: "REWARD_TASK_QUOTA_EXHAUSTED"}, retryLater},
		{&APIError{Status: 409, Reason: "REWARD_TASK_UNAVAILABLE"}, retryLater},
		{&APIError{Status: 409, Reason: "REWARD_TASK_ALREADY_COMPLETED"}, alreadyDone},
		{&APIError{Status: 400, Reason: "REWARD_TICKET_EXPIRED"}, rejected},
		{&APIError{Status: 409, Reason: "REWARD_EXTERNAL_USER_USED"}, rejected},
		{&APIError{Status: 400, Reason: "REWARD_EVENT_INVALID"}, rejected},
	}
	for _, tc := range cases {
		if got := classify(tc.err); got != tc.want {
			t.Errorf("classify(%v) = %d, want %d", tc.err, got, tc.want)
		}
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	good := map[string]string{
		"":                       DefaultBaseURL,
		"https://loxo.cc":        "https://loxo.cc",
		" https://LOXO.cc/ ":     "https://loxo.cc",
		"http://localhost:8080":  "http://localhost:8080",
		"http://127.0.0.1:18080": "http://127.0.0.1:18080",
	}
	for in, want := range good {
		if got, err := normalizeBaseURL(in); err != nil || got != want {
			t.Errorf("normalizeBaseURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"http://loxo.cc", "ftp://loxo.cc", "https://loxo.cc/api", "https://loxo.cc?x=1", "https://u:p@loxo.cc", "loxo.cc", "javascript:alert(1)"} {
		if _, err := normalizeBaseURL(in); err == nil {
			t.Errorf("normalizeBaseURL(%q) accepted", in)
		}
	}
}

func TestValidTicketAndBackoff(t *testing.T) {
	if !ValidTicket("rt_0123456789abcdef0123456789abcdef01234567") || ValidTicket("rt_short") || ValidTicket("xx_0123456789abcdef") ||
		ValidTicket("rt_0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcd") || ValidTicket("rt_01234567<script>") {
		t.Fatal("ticket validation mismatch")
	}
	if backoff(1) != 30*time.Second || backoff(2) != time.Minute || backoff(8) != time.Hour || backoff(48) != time.Hour {
		t.Fatalf("backoff = %v %v %v", backoff(1), backoff(2), backoff(8))
	}
}

func TestConfigReadyAndScriptURL(t *testing.T) {
	cfg := Config{Enabled: true, BaseURL: "https://loxo.cc", AppKey: "rk_1", Secret: "s", SDKEnabled: true}
	if !cfg.Ready() || cfg.SDKScriptURL() != "https://loxo.cc/reward-task-sdk.js" {
		t.Fatalf("ready config: %v %q", cfg.Ready(), cfg.SDKScriptURL())
	}
	cfg.SDKEnabled = false
	if cfg.SDKScriptURL() != "" {
		t.Fatal("sdk disabled must not expose script")
	}
	cfg.SDKEnabled, cfg.Secret = true, ""
	if cfg.Ready() || cfg.SDKScriptURL() != "" {
		t.Fatal("missing secret must not be ready")
	}
	s := &Service{}
	s.storeScriptOrigin(Config{Enabled: true, BaseURL: "https://loxo.cc", AppKey: "rk", Secret: "set", SDKEnabled: true})
	if got := s.ScriptOrigins(); len(got) != 1 || got[0] != "https://loxo.cc" {
		t.Fatalf("origins = %v", got)
	}
	s.storeScriptOrigin(Config{Enabled: false, BaseURL: "https://loxo.cc", AppKey: "rk", Secret: "set", SDKEnabled: true})
	if got := s.ScriptOrigins(); got != nil {
		t.Fatalf("disabled origins = %v", got)
	}
}
