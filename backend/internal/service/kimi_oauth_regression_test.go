//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/yx-token/token-market/internal/config"
	"github.com/yx-token/token-market/internal/pkg/kimi"
)

type kimiCASAccountRepo struct {
	tokenRefreshAccountRepo
	mu            sync.Mutex
	beforePersist func(*Account)
	beforeFailure func(*Account)
	recoverCalls  int
}

func (r *kimiCASAccountRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return snapshotOAuthRefreshAccount(r.accountsByID[id]), nil
}
func (r *kimiCASAccountRepo) UpdateKimiOAuthCredentialsIfUnchanged(_ context.Context, id int64, expected map[string]any, proxy *int64, creds map[string]any) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a := r.accountsByID[id]
	if r.beforePersist != nil {
		r.beforePersist(a)
	}
	if !reflect.DeepEqual(a.Credentials, expected) || !reflect.DeepEqual(a.ProxyID, proxy) {
		return false, nil
	}
	a.Credentials = shallowCopyMap(creds)
	return true, nil
}
func (r *kimiCASAccountRepo) SetKimiOAuthFailureIfUnchanged(_ context.Context, id int64, expected map[string]any, proxy *int64, permanent bool, until time.Time, reason string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a := r.accountsByID[id]
	if r.beforeFailure != nil {
		r.beforeFailure(a)
	}
	if !reflect.DeepEqual(a.Credentials, expected) || !reflect.DeepEqual(a.ProxyID, proxy) || a.Status != StatusActive {
		return false, nil
	}
	if permanent {
		a.Status = StatusError
		a.ErrorMessage = reason
	} else {
		a.TempUnschedulableUntil = &until
		a.TempUnschedulableReason = reason
	}
	return true, nil
}
func (r *kimiCASAccountRepo) RecoverKimiOAuthIfUnchanged(_ context.Context, id int64, expected map[string]any, proxy *int64) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a := r.accountsByID[id]
	if !reflect.DeepEqual(a.Credentials, expected) || !reflect.DeepEqual(a.ProxyID, proxy) {
		return false, nil
	}
	r.recoverCalls++
	if a.Status == StatusError {
		a.Status = StatusActive
	}
	a.ErrorMessage = ""
	a.TempUnschedulableUntil = nil
	a.TempUnschedulableReason = ""
	a.RateLimitResetAt = nil
	return true, nil
}
func newKimiRegressionAccount() *Account {
	return &Account{ID: 801, Platform: PlatformKimi, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Credentials: map[string]any{
		"access_token": "old-access", "refresh_token": "old-refresh", "expires_at": time.Now().Add(time.Minute).UTC().Format(time.RFC3339),
		"account_mode": AccountModeCoding, "api_protocol": APIProtocolChatCompletions, "base_url": kimi.DefaultBaseURL, "device_id": "device",
	}}
}
func newKimiRegressionProvider(a *Account, client KimiOAuthClient, cache GeminiTokenCache) (*KimiTokenProvider, *kimiCASAccountRepo, *KimiOAuthService) {
	r := &kimiCASAccountRepo{}
	r.accountsByID = map[int64]*Account{a.ID: snapshotOAuthRefreshAccount(a)}
	svc := NewKimiOAuthService(nil, client)
	p := NewKimiTokenProvider(r, cache)
	p.SetRefreshAPI(NewOAuthRefreshAPI(r, cache), NewKimiTokenRefresher(svc))
	svc.tokenProvider = p
	return p, r, svc
}
func rotatingKimiClient() *kimiOAuthClientStub {
	return &kimiOAuthClientStub{refresh: &kimi.TokenResponse{AccessToken: "new-access", RefreshToken: "new-refresh", ExpiresIn: 900}}
}

type kimiLockErrorCache struct{ grokTokenCacheForProviderTest }

func (*kimiLockErrorCache) AcquireRefreshLock(context.Context, string, time.Duration) (bool, error) {
	return false, errors.New("redis unavailable")
}

func TestKimiRefreshFailureStateAndCAS(t *testing.T) {
	for _, tc := range []struct {
		name            string
		failure         error
		missing, reauth bool
		permanent       bool
	}{
		{name: "invalid_grant", failure: kimi.ErrInvalidGrant, permanent: true},
		{name: "missing refresh token", missing: true, permanent: true},
		{name: "network", failure: errors.New("connection refused")},
		{name: "concurrent reauthorization wins", failure: kimi.ErrInvalidGrant, reauth: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newKimiRegressionAccount()
			a.Credentials["expires_at"] = time.Now().Add(-time.Second).Format(time.RFC3339)
			if tc.missing {
				delete(a.Credentials, "refresh_token")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
			defer cancel()
			p, r, _ := newKimiRegressionProvider(a, &kimiOAuthClientStub{refreshErr: tc.failure}, nil)
			if tc.reauth {
				r.beforeFailure = func(current *Account) {
					current.Credentials["access_token"] = "reauthorized"
					current.Credentials["refresh_token"] = "reauth-refresh"
				}
			}
			_, err := p.GetAccessToken(ctx, a)
			require.Error(t, err)
			current, _ := r.GetByID(context.Background(), a.ID)
			if tc.reauth {
				require.Equal(t, StatusActive, current.Status)
				require.Empty(t, current.ErrorMessage)
				return
			}
			if tc.permanent {
				require.Equal(t, StatusError, current.Status)
				require.Contains(t, current.ErrorMessage, "reauthorization required")
			} else {
				require.NotNil(t, current.TempUnschedulableUntil)
			}
		})
	}
}

func TestKimiRefreshLockFailureFailsClosed(t *testing.T) {
	for _, state := range []string{"valid", "expired", "missing"} {
		t.Run(state, func(t *testing.T) {
			a := newKimiRegressionAccount()
			if state == "expired" {
				a.Credentials["expires_at"] = time.Now().Add(-time.Second).Format(time.RFC3339)
			}
			if state == "missing" {
				delete(a.Credentials, "access_token")
			}
			client := rotatingKimiClient()
			p, _, _ := newKimiRegressionProvider(a, client, &kimiLockErrorCache{})
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
			defer cancel()
			token, err := p.GetAccessToken(ctx, a)
			if state != "valid" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, "old-access", token)
			}
			require.Zero(t, client.calls)
		})
	}
}

func TestKimiRefreshSuccessPreservesConcurrentSchedulingAndProtocol(t *testing.T) {
	a := newKimiRegressionAccount()
	a.Credentials["api_protocol"] = APIProtocolAnthropic
	a.Credentials["base_url"] = "https://relay.example/coding"
	p, r, _ := newKimiRegressionProvider(a, rotatingKimiClient(), nil)
	until := time.Now().Add(time.Hour)
	r.beforePersist = func(current *Account) {
		current.Status = StatusDisabled
		current.Schedulable = false
		current.TempUnschedulableUntil = &until
	}
	result, err := p.refreshAPI.RefreshIfNeeded(context.Background(), a, p.executor, kimiTokenRefreshSkew)
	require.NoError(t, err)
	require.True(t, result.Refreshed)
	require.Equal(t, StatusDisabled, result.Account.Status)
	require.False(t, result.Account.Schedulable)
	require.Equal(t, until, *result.Account.TempUnschedulableUntil)
	require.Equal(t, "https://relay.example/coding", result.Account.GetCredential("base_url"))
	scheduler := &tokenRefreshSchedulerCache{}
	s := &TokenRefreshService{accountRepo: r, schedulerCache: scheduler}
	s.postRefreshStateSync(context.Background(), a)
	require.Equal(t, StatusDisabled, scheduler.lastAccount.Status)
	require.False(t, scheduler.lastAccount.Schedulable)
}

func TestKimiConcurrentRefreshAndManualRecovery(t *testing.T) {
	a := newKimiRegressionAccount()
	a.Status = StatusError
	a.ErrorMessage = "reauthorization required"
	until := time.Now().Add(time.Hour)
	a.TempUnschedulableUntil = &until
	client := rotatingKimiClient()
	_, r, svc := newKimiRegressionProvider(a, client, nil)
	const workers = 12
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.RefreshAccount(context.Background(), snapshotOAuthRefreshAccount(a))
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, 1, client.calls)
	latest, _ := r.GetByID(context.Background(), a.ID)
	require.Equal(t, StatusActive, latest.Status)
	require.Empty(t, latest.ErrorMessage)
	require.Nil(t, latest.TempUnschedulableUntil)
	require.Equal(t, "new-refresh", latest.GetCredential("refresh_token"))
	require.Equal(t, 1, r.recoverCalls)
}

type kimiRegressionHTTP struct {
	HTTPUpstream
	requests []*http.Request
	statuses []int
}

func (u *kimiRegressionHTTP) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.requests = append(u.requests, req.Clone(req.Context()))
	status := 400
	if len(u.statuses) > 0 {
		status = u.statuses[0]
		u.statuses = u.statuses[1:]
	}
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":{"message":"fixture"}}`))}, nil
}

func TestKimiAllProtocolPathsPreRefresh(t *testing.T) {
	anthropic := []byte(`{"model":"kimi-k2","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`)
	chat := []byte(`{"model":"kimi-k2","messages":[{"role":"user","content":"hi"}]}`)
	responses := []byte(`{"model":"kimi-k2","input":"hi"}`)
	for _, name := range []string{"messages native", "chat native", "responses native", "messages fallback", "responses fallback", "raw chat"} {
		t.Run(name, func(t *testing.T) {
			a := newKimiRegressionAccount()
			if strings.Contains(name, "native") {
				a.Credentials["api_protocol"] = APIProtocolAnthropic
			}
			p, _, _ := newKimiRegressionProvider(a, rotatingKimiClient(), nil)
			up := &kimiRegressionHTTP{}
			s := &OpenAIGatewayService{kimiTokenProvider: p, httpUpstream: up, cfg: &config.Config{}}
			c, _ := newTestContext()
			ctx := context.Background()
			switch name {
			case "messages native":
				_, _ = s.forwardAnthropicViaNativeAnthropicEndpoint(ctx, c, a, anthropic, "")
			case "chat native":
				_, _ = s.forwardChatCompletionsViaNativeAnthropic(ctx, c, a, chat, "")
			case "responses native":
				_, _ = s.forwardResponsesViaNativeAnthropic(ctx, c, a, responses, "")
			case "messages fallback":
				_, _ = s.forwardAnthropicViaRawChatCompletions(ctx, c, a, anthropic, "")
			case "responses fallback":
				_, _ = s.forwardResponsesViaRawChatCompletions(ctx, c, a, responses)
			case "raw chat":
				_, _ = s.forwardAsRawChatCompletions(ctx, c, a, chat, "")
			}
			require.Len(t, up.requests, 1)
			require.Equal(t, "Bearer new-access", up.requests[0].Header.Get("Authorization"))
			require.Equal(t, "device", up.requests[0].Header.Get("X-Msh-Device-Id"))
			require.NotContains(t, up.requests[0].URL.Path, "v1/v1")
		})
	}
}
func TestKimiHTTP401RefreshesExactlyOnce(t *testing.T) {
	a := newKimiRegressionAccount()
	a.Credentials["expires_at"] = time.Now().Add(time.Hour).Format(time.RFC3339)
	p, _, _ := newKimiRegressionProvider(a, rotatingKimiClient(), nil)
	up := &kimiRegressionHTTP{statuses: []int{401, 401}}
	req, _ := http.NewRequest(http.MethodPost, "https://api.kimi.com/coding/v1/chat/completions", strings.NewReader(`{}`))
	resp, err := p.Do(req, a, func(r *http.Request) (*http.Response, error) { return up.Do(r, "", a.ID, 1) })
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, 401, resp.StatusCode)
	require.Len(t, up.requests, 2)
	require.Equal(t, "Bearer old-access", up.requests[0].Header.Get("Authorization"))
	require.Equal(t, "Bearer new-access", up.requests[1].Header.Get("Authorization"))
}
func TestKimiAdaptiveResponsesAndAnthropicBases(t *testing.T) {
	a := newKimiRegressionAccount()
	a.Credentials["api_protocol"] = APIProtocolAdaptive
	a.Credentials["api_base_urls"] = map[string]any{APIProtocolChatCompletions: "https://chat.example/v1", APIProtocolResponses: "https://responses.example/v1", APIProtocolAnthropic: kimi.DefaultBaseURL}
	require.Equal(t, DefaultKimiCodingAnthropicBaseURL, a.GetAnthropicProtocolBaseURL())
	s := &OpenAIGatewayService{cfg: &config.Config{}}
	c, _ := newTestContext()
	req, err := s.buildUpstreamRequest(context.Background(), c, a, []byte(`{}`), "token", false, "", false)
	require.NoError(t, err)
	require.Equal(t, "responses.example", req.URL.Host)
	ws, err := s.buildOpenAIResponsesWSURL(a)
	require.NoError(t, err)
	require.Contains(t, ws, "responses.example")
}

// The client is blocked in the first upstream poll, ensuring concurrent calls
// overlap instead of merely running one after another.
type kimiBlockingPollClient struct {
	kimiOAuthClientStub
	entered   chan struct{}
	release   chan struct{}
	pollCalls atomic.Int32
	proxy     string
}

func (c *kimiBlockingPollClient) Poll(_ context.Context, proxy, _, _ string) (kimi.PollStatus, *kimi.TokenResponse, int64, error) {
	c.pollCalls.Add(1)
	c.proxy = proxy
	close(c.entered)
	<-c.release
	return kimi.PollSuccess, &kimi.TokenResponse{AccessToken: "a", RefreshToken: "r", ExpiresIn: 900}, 0, nil
}
func TestKimiConcurrentPollSingleClaimAndPinnedProxy(t *testing.T) {
	c := &kimiBlockingPollClient{entered: make(chan struct{}), release: make(chan struct{})}
	svc := NewKimiOAuthService(nil, c)
	svc.sessionStore.Set("sid", &kimi.DeviceSession{DeviceCode: "code", DeviceID: "device", ProxyURL: "http://original.example:8080", ExpiresAt: time.Now().Add(time.Minute), Interval: 5})
	done := make(chan error, 1)
	go func() { _, err := svc.Poll(context.Background(), "sid", nil); done <- err }()
	<-c.entered
	changedProxy := int64(42)
	second, err := svc.Poll(context.Background(), "sid", &changedProxy)
	require.NoError(t, err)
	require.Equal(t, "pending", second.Status)
	close(c.release)
	require.NoError(t, <-done)
	require.EqualValues(t, 1, c.pollCalls.Load())
	require.Equal(t, "http://original.example:8080", c.proxy)
	_, err = svc.Poll(context.Background(), "sid", nil)
	require.Error(t, err)
}

func TestKimiProbeAndModelSyncUseProvider(t *testing.T) {
	a := newKimiRegressionAccount()
	p, _, _ := newKimiRegressionProvider(a, rotatingKimiClient(), nil)
	svc := &AccountTestService{kimiTokenProvider: p, cfg: &config.Config{}}
	req, err := svc.buildOpenAIUpstreamModelsRequest(context.Background(), a)
	require.NoError(t, err)
	require.Equal(t, "Bearer new-access", req.Header.Get("Authorization"))
	require.Empty(t, a.GetOpenAIProtocolAPIKey())
}

func TestKimiSlowDownBeyondSixtyAndPendingReleasesClaim(t *testing.T) {
	c := &kimiOAuthClientStub{pollStatus: kimi.PollSlowDown}
	svc := NewKimiOAuthService(nil, c)
	svc.sessionStore.Set("sid", &kimi.DeviceSession{DeviceCode: "d", ExpiresAt: time.Now().Add(time.Hour), Interval: 60})
	first, err := svc.Poll(context.Background(), "sid", nil)
	require.NoError(t, err)
	require.EqualValues(t, 65, first.Interval)
	second, err := svc.Poll(context.Background(), "sid", nil)
	require.NoError(t, err)
	require.EqualValues(t, 70, second.Interval)
	require.Equal(t, 2, c.calls)
}
func TestKimiRefreshRetryPolicyMatchesCLI(t *testing.T) {
	for _, tc := range []struct {
		name  string
		err   error
		calls int
	}{
		{"bad request", &kimi.OAuthError{Status: 400, Code: "invalid_request"}, 1},
		{"invalid token response", kimi.ErrInvalidTokenResponse, 1},
		{"unauthorized", kimi.ErrInvalidGrant, 1},
		{"transient", &kimi.OAuthError{Status: 503, Code: "temporarily_unavailable"}, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &kimiOAuthClientStub{refreshErr: tc.err}
			svc := NewKimiOAuthService(nil, client)
			_, err := svc.RefreshToken(context.Background(), "refresh", nil, "device")
			require.Error(t, err)
			require.Equal(t, tc.calls, client.calls)
		})
	}
}

func TestKimiManualRefreshAlsoFailsClosedOnRedisError(t *testing.T) {
	a := newKimiRegressionAccount()
	client := rotatingKimiClient()
	_, _, svc := newKimiRegressionProvider(a, client, &kimiLockErrorCache{})
	_, err := svc.RefreshAccount(context.Background(), a)
	require.Error(t, err)
	require.Zero(t, client.calls)
}

type kimiRegressionDialer struct{ headers []http.Header }

func (d *kimiRegressionDialer) Dial(_ context.Context, _ string, headers http.Header, _ string) (openAIWSClientConn, int, http.Header, error) {
	d.headers = append(d.headers, headers.Clone())
	if len(d.headers) == 1 {
		return nil, 401, nil, errors.New("unauthorized")
	}
	return nil, 101, nil, nil
}
func TestKimiWebsocketHandshakeRefreshesOnce(t *testing.T) {
	a := newKimiRegressionAccount()
	a.Credentials["expires_at"] = time.Now().Add(time.Hour).Format(time.RFC3339)
	client := rotatingKimiClient()
	p, _, _ := newKimiRegressionProvider(a, client, nil)
	dialer := &kimiRegressionDialer{}
	_, status, _, err := p.Dial(context.Background(), a, "wss://api.kimi.com/coding/v1/responses", http.Header{}, "", dialer)
	require.NoError(t, err)
	require.Equal(t, 101, status)
	require.Len(t, dialer.headers, 2)
	require.Equal(t, "Bearer old-access", dialer.headers[0].Get("Authorization"))
	require.Equal(t, "Bearer new-access", dialer.headers[1].Get("Authorization"))
	require.Equal(t, 1, client.calls)
}

func TestKimiSuccessfulRotationDoesNotOverwriteReauthorization(t *testing.T) {
	a := newKimiRegressionAccount()
	p, r, _ := newKimiRegressionProvider(a, rotatingKimiClient(), nil)
	r.beforePersist = func(current *Account) {
		current.Credentials = shallowCopyMap(current.Credentials)
		current.Credentials["access_token"] = "reauthorized"
		current.Credentials["refresh_token"] = "reauth-refresh"
		current.Credentials["expires_at"] = time.Now().Add(time.Hour).Format(time.RFC3339)
	}
	token, err := p.GetAccessToken(context.Background(), a)
	require.NoError(t, err)
	require.Equal(t, "reauthorized", token)
	current, _ := r.GetByID(context.Background(), a.ID)
	require.Equal(t, "reauth-refresh", current.GetCredential("refresh_token"))
}
func TestKimiBackgroundFailureDoesNotFallThroughToUnconditionalMutations(t *testing.T) {
	a := newKimiRegressionAccount()
	p, r, _ := newKimiRegressionProvider(a, &kimiOAuthClientStub{refreshErr: kimi.ErrInvalidGrant}, nil)
	r.beforeFailure = func(current *Account) {
		current.Credentials["access_token"] = "reauthorized"
		current.Credentials["refresh_token"] = "reauth-refresh"
	}
	s := &TokenRefreshService{accountRepo: r, refreshAPI: p.refreshAPI}
	err := s.refreshWithRetry(context.Background(), a, p.executor, p.executor, kimiTokenRefreshSkew)
	require.Error(t, err)
	current, _ := r.GetByID(context.Background(), a.ID)
	require.Equal(t, StatusActive, current.Status)
	require.Zero(t, r.setErrorCalls)
	require.Zero(t, r.setTempUnschedCalls)
}
func TestKimiAdminConnectionRetries401BeforeAnyErrorMutation(t *testing.T) {
	a := newKimiRegressionAccount()
	a.Credentials["expires_at"] = time.Now().Add(time.Hour).Format(time.RFC3339)
	p, r, _ := newKimiRegressionProvider(a, rotatingKimiClient(), nil)
	up := &kimiRegressionHTTP{statuses: []int{401, 401}}
	svc := &AccountTestService{kimiTokenProvider: p, accountRepo: r, httpUpstream: up, cfg: &config.Config{}}
	c, _ := newTestContext()
	_ = svc.testCNProviderChatCompletionsConnection(c, a, "kimi-k2", "hi")
	require.Len(t, up.requests, 2)
	require.Equal(t, "Bearer new-access", up.requests[1].Header.Get("Authorization"))
	require.Zero(t, r.setErrorCalls)
	current, _ := r.GetByID(context.Background(), a.ID)
	require.Equal(t, StatusActive, current.Status)
	require.NotNil(t, current.TempUnschedulableUntil)
}
func TestKimiQuotaProbePreRefreshes(t *testing.T) {
	a := newKimiRegressionAccount()
	p, r, _ := newKimiRegressionProvider(a, rotatingKimiClient(), nil)
	up := &kimiRegressionHTTP{statuses: []int{200}}
	svc := NewCNProviderQuotaService(r, nil, up, &config.Config{})
	svc.kimiTokens = p
	_, err := svc.QueryUsageForAccount(context.Background(), a)
	require.NoError(t, err)
	require.Len(t, up.requests, 1)
	require.Equal(t, "Bearer new-access", up.requests[0].Header.Get("Authorization"))
	require.Equal(t, "/coding/v1/usages", up.requests[0].URL.Path)
}
func TestKimiManualRecoveryClearsRedisAndRuntimeBlocks(t *testing.T) {
	a := newKimiRegressionAccount()
	a.Status = StatusError
	p, _, svc := newKimiRegressionProvider(a, rotatingKimiClient(), nil)
	cache := &tempUnschedCacheStub{}
	blocker := &tokenRefreshRuntimeBlocker{}
	p.tempUnschedCache = cache
	p.runtimeBlocker = blocker
	_, err := svc.RefreshAccount(context.Background(), a)
	require.NoError(t, err)
	require.Equal(t, 1, cache.deleteCalls)
	require.Equal(t, 1, blocker.clearCalls)
}

func TestKimiRateLimit401CannotOverwriteNewAuthorization(t *testing.T) {
	a := newKimiRegressionAccount()
	_, r, _ := newKimiRegressionProvider(a, rotatingKimiClient(), nil)
	r.accountsByID[a.ID].Credentials["access_token"] = "reauthorized"
	svc := &RateLimitService{accountRepo: r, cfg: &config.Config{}}
	svc.handleKimiOAuthUnauthorized(context.Background(), a)
	current, _ := r.GetByID(context.Background(), a.ID)
	require.Equal(t, StatusActive, current.Status)
	require.Nil(t, current.TempUnschedulableUntil)
	require.Zero(t, r.setErrorCalls)
	require.Zero(t, r.setTempUnschedCalls)
}
