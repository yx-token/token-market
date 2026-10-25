//go:build unit

package service

import (
	"context"
	"maps"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/yx-token/token-market/internal/pkg/zhipu"
)

type zhipuAccountRepoTest struct {
	AccountRepository
	mu            sync.Mutex
	a             *Account
	beforeCAS     func(*Account)
	writes, marks int
}

func (r *zhipuAccountRepoTest) GetByID(context.Context, int64) (*Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a := *r.a
	a.Credentials = maps.Clone(r.a.Credentials)
	return &a, nil
}

func (r *zhipuAccountRepoTest) Create(_ context.Context, a *Account) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	a.ID = 701
	r.a = snapshotOAuthRefreshAccount(a)
	return nil
}

func (r *zhipuAccountRepoTest) Update(_ context.Context, a *Account) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.a = snapshotOAuthRefreshAccount(a)
	return nil
}

func (r *zhipuAccountRepoTest) UpdateExtra(context.Context, int64, map[string]any) error { return nil }
func (r *zhipuAccountRepoTest) UpdateZhipuOAuthCredentialsIfUnchanged(_ context.Context, _ int64, expected map[string]any, proxy *int64, creds map[string]any) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.beforeCAS != nil {
		r.beforeCAS(r.a)
		r.beforeCAS = nil
	}
	if !reflect.DeepEqual(expected, r.a.Credentials) || !reflect.DeepEqual(proxy, r.a.ProxyID) {
		return false, nil
	}
	r.a.Credentials = maps.Clone(creds)
	r.writes++
	return true, nil
}
func (r *zhipuAccountRepoTest) SetZhipuOAuthReauthorizationRequiredIfUnchanged(_ context.Context, _ int64, expected map[string]any, proxy *int64, message string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !reflect.DeepEqual(expected, r.a.Credentials) || !reflect.DeepEqual(proxy, r.a.ProxyID) {
		return false, nil
	}
	r.a.Status = StatusError
	r.a.Schedulable = false
	r.a.ErrorMessage = message
	r.marks++
	return true, nil
}

func zhipuTestAccount(provider zhipu.Provider) *Account {
	return &Account{ID: 701, Platform: PlatformZhipu, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Credentials: map[string]any{
		"api_key": "old.secret", "access_token": "business", "zai_oauth_access_token": "zai-oauth", "oauth_provider": string(provider), "business_expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339), "zcode_jwt": "unknown-jwt", "refresh_token": "preserve-refresh", "account_mode": AccountModeCoding, "api_protocol": APIProtocolChatCompletions,
	}}
}

func zhipuTestProvider(f *zhipuOAuthFixture, a *Account) (*ZhipuTokenProvider, *zhipuAccountRepoTest) {
	repo := &zhipuAccountRepoTest{a: snapshotOAuthRefreshAccount(a)}
	return NewZhipuTokenProvider(repo, NewOAuthRefreshAPI(repo, nil), f.service), repo
}

func TestZhipuRefreshReusesBusinessAndMergesCredentials(t *testing.T) {
	f := newZhipuOAuthFixture(t)
	a := zhipuTestAccount(zhipu.ProviderZai)
	a.Credentials["_token_version"] = time.Now().Add(time.Hour).UnixMilli()
	p, repo := zhipuTestProvider(f, a)
	oldVersion := a.GetCredentialAsInt64("_token_version")
	result, err := p.Account(context.Background(), a, true)
	require.NoError(t, err)
	require.Equal(t, "derived.secret", result.GetCredential("api_key"))
	require.Zero(t, f.logins.Load())
	require.Equal(t, 1, repo.writes)
	require.Equal(t, "preserve-refresh", result.GetCredential("refresh_token"))
	require.Equal(t, "unknown-jwt", result.GetCredential("zcode_jwt"))
	require.Greater(t, result.GetCredentialAsInt64("_token_version"), oldVersion)
	_, err = p.Account(context.Background(), result, false)
	require.NoError(t, err)
	require.EqualValues(t, 1, f.copies.Load())
}

func TestZhipuRefreshExpiredAndUnknownBusinessTTL(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(map[bool]string{false: "expired", true: "unknown"}[unknown], func(t *testing.T) {
			f := newZhipuOAuthFixture(t)
			a := zhipuTestAccount(zhipu.ProviderZai)
			a.Credentials["business_expires_at"] = time.Now().Add(-time.Hour).Format(time.RFC3339)
			if unknown {
				f.loginTTL.Store(0)
			}
			p, _ := zhipuTestProvider(f, a)
			result, err := p.Account(context.Background(), a, false)
			require.NoError(t, err)
			require.EqualValues(t, 1, f.logins.Load())
			require.Equal(t, "zai-business", result.GetCredential("access_token"))
			require.Equal(t, unknown, NewZhipuTokenRefresher(f.service).NeedsRefresh(result, 5*time.Minute))
			_, err = p.Account(context.Background(), result, false)
			require.NoError(t, err)
			want := int32(1)
			if unknown {
				want = 2
			}
			require.Equal(t, want, f.logins.Load())
		})
	}
}

func TestZhipuRefreshReauthorizationAndCAS(t *testing.T) {
	f := newZhipuOAuthFixture(t)
	a := zhipuTestAccount(zhipu.ProviderBigModel)
	p, repo := zhipuTestProvider(f, a)
	f.denyBusiness.Store(true)
	_, err := p.Account(context.Background(), a, true)
	require.ErrorIs(t, err, ErrZhipuReauthorizationRequired)
	require.Equal(t, StatusError, repo.a.Status)
	require.False(t, repo.a.Schedulable)
	require.Equal(t, 1, repo.marks)
	_, err = p.Account(context.Background(), repo.a, true)
	require.ErrorIs(t, err, ErrZhipuReauthorizationRequired)
	// A stale failure cannot quarantine a newly authorized credential document.
	fresh := zhipuTestAccount(zhipu.ProviderBigModel)
	fresh.Credentials["api_key"] = "new.secret"
	repo.a = fresh
	require.NoError(t, p.RequireReauthorization(context.Background(), a))
	require.Equal(t, StatusActive, repo.a.Status)
}

func TestZhipuRefreshConcurrentAndSupersededSuccess(t *testing.T) {
	f := newZhipuOAuthFixture(t)
	a := zhipuTestAccount(zhipu.ProviderBigModel)
	p, repo := zhipuTestProvider(f, a)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() { _, err := p.Account(context.Background(), a, true); errs <- err })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.EqualValues(t, 1, f.copies.Load())
	require.Equal(t, 1, repo.writes)
	before, _ := repo.GetByID(context.Background(), a.ID)
	repo.beforeCAS = func(current *Account) { current.Credentials["api_key"] = "reauthorized.secret" }
	result, err := p.Account(context.Background(), before, true)
	require.NoError(t, err)
	require.Equal(t, "reauthorized.secret", result.GetCredential("api_key"))
	require.Equal(t, 1, repo.writes)
}

type zhipuRemoteRefreshCache struct {
	GeminiTokenCache
	onAcquire func(string)
}

func (c zhipuRemoteRefreshCache) AcquireRefreshLock(_ context.Context, key string, _ time.Duration) (bool, error) {
	c.onAcquire(key)
	return false, nil
}

func TestZhipuRefreshWaitsForDistributedLockOwner(t *testing.T) {
	f := newZhipuOAuthFixture(t)
	a := zhipuTestAccount(zhipu.ProviderBigModel)
	repo := &zhipuAccountRepoTest{a: snapshotOAuthRefreshAccount(a)}
	cache := zhipuRemoteRefreshCache{onAcquire: func(key string) {
		require.Equal(t, "zhipu:account:701", key)
		repo.mu.Lock()
		defer repo.mu.Unlock()
		// Generic admin reauthorization may replace credentials without adding a
		// token version. A waiting request must recognize that new document too.
		repo.a.Credentials["api_key"] = "reauthorized.secret"
	}}
	p := NewZhipuTokenProvider(repo, NewOAuthRefreshAPI(repo, cache), f.service)
	result, err := p.Account(context.Background(), a, true)
	require.NoError(t, err)
	require.Equal(t, "reauthorized.secret", result.GetCredential("api_key"))
	require.Zero(t, f.copies.Load())
}
