package kimi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDeviceAuthorizationAndHeaders(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		if r.URL.Path != "/api/oauth/device_authorization" || r.Method != http.MethodPost {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseForm(); err != nil || r.Form.Get("client_id") != ClientID {
			t.Errorf("form client_id missing: %v", r.Form)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"user_code":"ABCD","device_code":"device","verification_uri":"https://auth.kimi.com/verify","verification_uri_complete":"https://auth.kimi.com/verify?code=ABCD","expires_in":900,"interval":5}`))
	}))
	defer srv.Close()
	c := NewClient("")
	c.Host = srv.URL
	gotDevice, err := c.DeviceAuthorization(context.Background(), "device-id")
	if err != nil {
		t.Fatal(err)
	}
	if gotDevice.UserCode != "ABCD" || gotDevice.DeviceCode != "device" {
		t.Fatalf("unexpected device response: %+v", gotDevice)
	}
	if got.Get("X-Msh-Platform") != "kimi_code_cli" || got.Get("X-Msh-Version") != CLIVersion || got.Get("X-Msh-Device-Id") != "device-id" {
		t.Fatalf("identity headers missing: %v", got)
	}
	if !strings.HasPrefix(got.Get("User-Agent"), "kimi-code-cli/") {
		t.Fatalf("unexpected user agent: %s", got.Get("User-Agent"))
	}
}

func TestPollStatuses(t *testing.T) {
	cases := []struct {
		body   string
		status int
		want   PollStatus
	}{
		{`{"error":"authorization_pending"}`, http.StatusBadRequest, PollPending},
		{`{"error":"slow_down"}`, http.StatusBadRequest, PollSlowDown},
		{`{"error":"expired_token"}`, http.StatusBadRequest, PollExpired},
		{`{"error":"access_denied"}`, http.StatusBadRequest, PollDenied},
		{`{"access_token":"a","refresh_token":"r","expires_in":900}`, http.StatusOK, PollSuccess},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		}))
		c := NewClient("")
		c.Host = srv.URL
		got, _, _, err := c.Poll(context.Background(), "device", "id")
		srv.Close()
		if tc.want == PollExpired || tc.want == PollDenied {
			if err == nil {
				t.Errorf("%s expected terminal error", tc.want)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", tc.want, err)
		}
		if got != tc.want {
			t.Errorf("got %s want %s", got, tc.want)
		}
	}
}

func TestRefreshInvalidGrant(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer srv.Close()
	c := NewClient("")
	c.Host = srv.URL
	_, err := c.Refresh(context.Background(), "refresh", "id")
	if err != ErrInvalidGrant {
		t.Fatalf("got %v want ErrInvalidGrant", err)
	}
}
