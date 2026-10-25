package service

import (
	"context"
	"testing"
	"time"

	"github.com/yx-token/token-market/internal/pkg/kimi"
	"github.com/yx-token/token-market/internal/pkg/pagination"
)

type kimiProxyRepoStub struct {
	proxy *Proxy
}

func (r kimiProxyRepoStub) Create(context.Context, *Proxy) error                { return nil }
func (r kimiProxyRepoStub) GetByID(context.Context, int64) (*Proxy, error)      { return r.proxy, nil }
func (r kimiProxyRepoStub) ListByIDs(context.Context, []int64) ([]Proxy, error) { return nil, nil }
func (r kimiProxyRepoStub) Update(context.Context, *Proxy) error                { return nil }
func (r kimiProxyRepoStub) Delete(context.Context, int64) error                 { return nil }
func (r kimiProxyRepoStub) List(context.Context, pagination.PaginationParams) ([]Proxy, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (r kimiProxyRepoStub) ListWithFilters(context.Context, pagination.PaginationParams, string, string, string) ([]Proxy, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (r kimiProxyRepoStub) ListWithFiltersAndAccountCount(context.Context, pagination.PaginationParams, string, string, string) ([]ProxyWithAccountCount, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (r kimiProxyRepoStub) ListActive(context.Context) ([]Proxy, error) { return nil, nil }
func (r kimiProxyRepoStub) ListActiveWithAccountCount(context.Context) ([]ProxyWithAccountCount, error) {
	return nil, nil
}
func (r kimiProxyRepoStub) ExistsByHostPortAuth(context.Context, string, int, string, string) (bool, error) {
	return false, nil
}
func (r kimiProxyRepoStub) CountAccountsByProxyID(context.Context, int64) (int64, error) {
	return 0, nil
}
func (r kimiProxyRepoStub) ListAccountSummariesByProxyID(context.Context, int64) ([]ProxyAccountSummary, error) {
	return nil, nil
}
func (r kimiProxyRepoStub) SweepExpiredProxies(context.Context, time.Time) (int64, error) {
	return 0, nil
}
func (r kimiProxyRepoStub) ListAllForFallback(context.Context) ([]Proxy, error) { return nil, nil }
func (r kimiProxyRepoStub) CountExpired(context.Context) (int64, error)         { return 0, nil }
func (r kimiProxyRepoStub) CountExpiringSoon(context.Context, time.Time) (int64, error) {
	return 0, nil
}

type kimiOAuthClientStub struct {
	device     *kimi.DeviceAuthorizationResponse
	pollStatus kimi.PollStatus
	token      *kimi.TokenResponse
	refresh    *kimi.TokenResponse
	refreshErr error
	calls      int
}

func (s *kimiOAuthClientStub) DeviceAuthorization(context.Context, string, string) (*kimi.DeviceAuthorizationResponse, error) {
	return s.device, nil
}
func (s *kimiOAuthClientStub) Poll(context.Context, string, string, string) (kimi.PollStatus, *kimi.TokenResponse, int64, error) {
	s.calls++
	return s.pollStatus, s.token, 5, nil
}
func (s *kimiOAuthClientStub) Refresh(context.Context, string, string, string) (*kimi.TokenResponse, error) {
	s.calls++
	return s.refresh, s.refreshErr
}

func TestKimiOAuthServiceDevicePollConsumesSession(t *testing.T) {
	stub := &kimiOAuthClientStub{device: &kimi.DeviceAuthorizationResponse{UserCode: "U", DeviceCode: "D", VerificationURI: "https://auth.kimi.com/v", VerificationURIComplete: "https://auth.kimi.com/v?c=U", ExpiresIn: 900, Interval: 5}, pollStatus: kimi.PollSuccess, token: &kimi.TokenResponse{AccessToken: "a", RefreshToken: "r", ExpiresIn: 900, Scope: "kimi-code"}}
	svc := NewKimiOAuthService(nil, stub)
	device, err := svc.DeviceCode(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Poll(context.Background(), device.SessionID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "success" || result.TokenInfo == nil || result.TokenInfo.DeviceID == "" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if _, err := svc.Poll(context.Background(), device.SessionID, nil); err == nil {
		t.Fatal("consumed session should not be reusable")
	}
}

func TestKimiOAuthServiceStoresProxyIDInDeviceSession(t *testing.T) {
	proxyID := int64(42)
	stub := &kimiOAuthClientStub{device: &kimi.DeviceAuthorizationResponse{
		UserCode: "U", DeviceCode: "D", VerificationURIComplete: "https://auth.kimi.com/v?c=U", ExpiresIn: 900, Interval: 5,
	}}
	svc := NewKimiOAuthService(kimiProxyRepoStub{proxy: &Proxy{Protocol: "http", Host: "127.0.0.1", Port: 8080}}, stub)
	result, err := svc.DeviceCode(context.Background(), &proxyID)
	if err != nil {
		t.Fatal(err)
	}
	session, ok := svc.sessionStore.Get(result.SessionID)
	if !ok || session.ProxyID == nil || *session.ProxyID != proxyID {
		t.Fatalf("proxy id was not persisted in device session: %+v", session)
	}
}

func TestKimiOAuthServiceRejectsEmptyDeviceResponse(t *testing.T) {
	svc := NewKimiOAuthService(nil, &kimiOAuthClientStub{})
	if _, err := svc.DeviceCode(context.Background(), nil); err == nil {
		t.Fatal("empty device response should fail")
	}
}

func TestKimiOAuthServiceRejectsEmptyPollToken(t *testing.T) {
	stub := &kimiOAuthClientStub{
		device:     &kimi.DeviceAuthorizationResponse{UserCode: "U", DeviceCode: "D", VerificationURIComplete: "https://auth.kimi.com/v?c=U", ExpiresIn: 900, Interval: 5},
		pollStatus: kimi.PollSuccess,
	}
	svc := NewKimiOAuthService(nil, stub)
	device, err := svc.DeviceCode(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Poll(context.Background(), device.SessionID, nil)
	if err != nil || result.Status != "failed" || result.Message == "" {
		t.Fatalf("incomplete token should return failed: %+v %v", result, err)
	}
}

func TestKimiOAuthServiceSlowDownIncreasesInterval(t *testing.T) {
	stub := &kimiOAuthClientStub{device: &kimi.DeviceAuthorizationResponse{UserCode: "U", DeviceCode: "D", VerificationURIComplete: "https://auth.kimi.com/v?c=U", ExpiresIn: 900, Interval: 5}, pollStatus: kimi.PollSlowDown}
	svc := NewKimiOAuthService(nil, stub)
	device, err := svc.DeviceCode(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Poll(context.Background(), device.SessionID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "slow_down" || result.Interval != 10 {
		t.Fatalf("unexpected slow_down result: %+v", result)
	}
}

func TestKimiOAuthServiceExpiredSessionReturnsTerminalStatus(t *testing.T) {
	stub := &kimiOAuthClientStub{device: &kimi.DeviceAuthorizationResponse{
		UserCode: "U", DeviceCode: "D", VerificationURIComplete: "https://auth.kimi.com/v?c=U", ExpiresIn: 900, Interval: 5,
	}}
	svc := NewKimiOAuthService(nil, stub)
	device, err := svc.DeviceCode(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	session, ok := svc.sessionStore.GetIncludingExpired(device.SessionID)
	if !ok {
		t.Fatal("device session not stored")
	}
	session.ExpiresAt = time.Now().Add(-time.Second)
	if err := svc.sessionStore.Set(device.SessionID, session); err != nil {
		t.Fatal(err)
	}
	result, err := svc.Poll(context.Background(), device.SessionID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != string(kimi.PollExpired) {
		t.Fatalf("unexpected expired result: %+v", result)
	}
}

func TestKimiBuildAccountCredentials(t *testing.T) {
	info := (&KimiOAuthService{}).tokenInfo(&kimi.TokenResponse{AccessToken: "a", RefreshToken: "r", ExpiresIn: 900, Scope: "kimi-code"}, "device")
	creds := (&KimiOAuthService{}).BuildAccountCredentials(info)
	if creds["access_token"] != "a" || creds["refresh_token"] != "r" || creds["account_mode"] != AccountModeCoding || creds["base_url"] != kimi.DefaultBaseURL || creds["device_id"] != "device" {
		t.Fatalf("unexpected credentials: %#v", creds)
	}
	expiresAt, ok := creds["expires_at"].(string)
	if !ok {
		t.Fatalf("expires_at must be a string: %#v", creds["expires_at"])
	}
	if _, err := time.Parse(time.RFC3339, expiresAt); err != nil {
		t.Fatalf("expires_at must be RFC3339: %v", err)
	}
}
