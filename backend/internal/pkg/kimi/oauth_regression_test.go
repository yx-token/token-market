//go:build unit

package kimi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestKimiTokenValidationAndHTTP200Only(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		want       PollStatus
	}{
		{"valid", `{"access_token":"a","refresh_token":"r","expires_in":900}`, 200, PollSuccess},
		{"missing refresh", `{"access_token":"a","expires_in":900}`, 200, PollFailed},
		{"missing expiry", `{"access_token":"a","refresh_token":"r"}`, 200, PollFailed},
		{"invalid expiry", `{"access_token":"a","refresh_token":"r","expires_in":-1}`, 200, PollFailed},
		{"wrong success status", `{"access_token":"a","refresh_token":"r","expires_in":900}`, 201, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			c := NewClient("")
			c.Host = server.URL
			status, _, _, err := c.Poll(context.Background(), "device", "id")
			if tc.want == "" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.want, status)
			}
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"device_code":"d","user_code":"u","verification_uri_complete":"https://auth.kimi.com/v"}`))
	}))
	defer server.Close()
	c := NewClient("")
	c.Host = server.URL
	_, err := c.DeviceAuthorization(context.Background(), "id")
	require.Error(t, err)
}

func TestKimiErrorsNeverReturnRawBodiesOrSecrets(t *testing.T) {
	for _, status := range []int{400, 500, 502} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":"temporarily_unavailable","error_description":"refresh_token=secret device_code=device-secret Bearer secret","access_token":"private-access","unexpected":"never-expose-this"}`))
			}))
			defer server.Close()
			c := NewClient("")
			c.Host = server.URL
			_, err := c.Refresh(context.Background(), "secret", "id")
			require.Error(t, err)
			require.NotContains(t, err.Error(), "secret")
			require.NotContains(t, err.Error(), "private-access")
			require.NotContains(t, err.Error(), "never-expose-this")
			var parsed *OAuthError
			require.ErrorAs(t, err, &parsed)
			require.Equal(t, status, parsed.Status)
		})
	}
	for _, status := range []int{401, 403} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
		c := NewClient("")
		c.Host = server.URL
		_, err := c.Refresh(context.Background(), "r", "id")
		server.Close()
		require.ErrorIs(t, err, ErrInvalidGrant)
	}
}

func TestKimiRedisLifetimeAndClaimsAcrossInstances(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	a, b := NewRedisSessionStore(rdb), NewRedisSessionStore(rdb)
	require.NoError(t, a.Set("sid", &DeviceSession{DeviceCode: "d", ExpiresAt: time.Now().Add(37 * time.Minute)}))
	require.InDelta(t, (37 * time.Minute).Seconds(), mr.TTL("oauth:session:kimi:sid").Seconds(), 1)
	var claims atomic.Int32
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store := a
			if i%2 == 0 {
				store = b
			}
			ok, err := store.Claim(context.Background(), "sid")
			if err == nil && ok {
				claims.Add(1)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, claims.Load())
	a.Release(context.Background(), "sid")
	ok, err := b.Claim(context.Background(), "sid")
	require.NoError(t, err)
	require.True(t, ok)
	b.Delete("sid")
	ok, err = a.Claim(context.Background(), "sid")
	require.NoError(t, err)
	require.False(t, ok)
	require.NoError(t, a.Set("short", &DeviceSession{DeviceCode: "d", ExpiresAt: time.Now().Add(20 * time.Second)}))
	mr.FastForward(21 * time.Second)
	_, ok = a.GetIncludingExpired("short")
	require.False(t, ok)
}

func TestKimiMemorySessionUpdatePreservesPollClaim(t *testing.T) {
	store := NewSessionStore()
	session := &DeviceSession{DeviceCode: "d", Interval: 5, ExpiresAt: time.Now().Add(time.Minute)}
	require.NoError(t, store.Set("sid", session))
	claimed, err := store.Claim(context.Background(), "sid")
	require.NoError(t, err)
	require.True(t, claimed)
	// Poll's snapshot predates its claim, as with a slow_down interval update.
	session.Interval += 5
	require.NoError(t, store.Set("sid", session))
	claimed, err = store.Claim(context.Background(), "sid")
	require.NoError(t, err)
	require.False(t, claimed)
	store.Release(context.Background(), "sid")
	claimed, err = store.Claim(context.Background(), "sid")
	require.NoError(t, err)
	require.True(t, claimed)
}

func TestKimiDeviceIdentityFormatAndOverrides(t *testing.T) {
	t.Setenv("KIMI_CODE_DEVICE_MODEL", "Linux 6.12.0 x64")
	t.Setenv("KIMI_CODE_OS_VERSION", "6.12.0")
	t.Setenv("KIMI_CODE_DEVICE_NAME", "gateway-01")
	h := make(http.Header)
	ApplyDeviceHeaders(h, "stable-device")
	require.Equal(t, "Linux 6.12.0 x64", h.Get("X-Msh-Device-Model"))
	require.Equal(t, "6.12.0", h.Get("X-Msh-Os-Version"))
	require.Equal(t, "gateway-01", h.Get("X-Msh-Device-Name"))
	require.Equal(t, "stable-device", h.Get("X-Msh-Device-Id"))
	require.True(t, errors.Is(ValidateToken(nil), ErrInvalidTokenResponse))
}
