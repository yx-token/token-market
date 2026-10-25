//go:build unit

package zhipu

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestRedisOAuthSessionAtomicConsumption(t *testing.T) {
	r := miniredis.RunT(t)
	c := redis.NewClient(&redis.Options{Addr: r.Addr()})
	t.Cleanup(func() { _ = c.Close() })
	s := NewRedisOAuthSessionStore(c)
	ctx := context.Background()
	require.NoError(t, s.Create(ctx, "id", &OAuthSession{Status: "pending", ExpiresAt: time.Now().Add(time.Minute), PollToken: "private"}))
	require.ErrorIs(t, s.Create(ctx, "id", &OAuthSession{ExpiresAt: time.Now().Add(time.Minute)}), ErrSessionBusy)
	var won atomic.Int32
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			_, err := s.Acquire(ctx, "id", string(rune('a'+i)))
			if err == nil {
				won.Add(1)
			} else {
				require.ErrorIs(t, err, ErrSessionBusy)
			}
		})
	}
	wg.Wait()
	require.EqualValues(t, 1, won.Load())
	var claimed OAuthSession
	raw, err := r.Get("zhipu:oauth:session:id")
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal([]byte(raw), &claimed))
	require.NotEmpty(t, claimed.Owner)
	owner := claimed.Owner
	require.ErrorIs(t, s.Save(ctx, "id", "imposter", &claimed), ErrSessionExpired)
	claimed.Status, claimed.PollToken, claimed.Result = "success", "", json.RawMessage(`{"api_key":"id.secret"}`)
	r.FastForward(10 * time.Second)
	ttl := r.TTL("zhipu:oauth:session:id")
	require.NoError(t, s.Save(ctx, "id", owner, &claimed))
	require.Equal(t, ttl, r.TTL("zhipu:oauth:session:id"))
	for range 2 {
		final, err := s.Acquire(ctx, "id", "reader")
		require.NoError(t, err)
		require.Equal(t, "success", final.Status)
		require.Empty(t, final.PollToken)
		require.JSONEq(t, `{"api_key":"id.secret"}`, string(final.Result))
	}
	r.FastForward(time.Minute)
	_, err = s.Acquire(ctx, "id", "reader")
	require.ErrorIs(t, err, ErrSessionExpired)
}
