package buyer

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
	"github.com/yx-token/token-market/market/internal/scheduling"
)

func TestBuildDeeplink(t *testing.T) {
	link, err := BuildDeeplink("abc123", "codex", "macos", "https://idlicat.com/")
	require.NoError(t, err)
	require.Equal(t, "xianmao://one-click?api=https%3A%2F%2Fidlicat.com&client=codex&os=macos&ticket=abc123", link)
	require.NotContains(t, link, "sk-")
}

func TestNormalizeRejectsBadInput(t *testing.T) {
	_, err := normalizeOneClickClient("evil")
	require.Error(t, err)
	_, err = normalizeAPIBase("ftp://x")
	require.Error(t, err)
	_, err = normalizeAPIBase("/relative")
	require.Error(t, err)
}

func TestPickExchangeBaseURLPrefersReachableOrigin(t *testing.T) {
	require.Equal(t, "http://172.16.76.52:8080", pickExchangeBaseURL("http://127.0.0.1:8080", "http://172.16.76.52:8080"))
	require.Equal(t, "https://idlicat.com", pickExchangeBaseURL("https://idlicat.com", "https://idlicat.com"))
	require.Equal(t, "https://api.idlicat.com", pickExchangeBaseURL("https://api.idlicat.com", "http://127.0.0.1:8080"))
	require.Equal(t, "http://172.16.76.52:8080", pickExchangeBaseURL("", "http://172.16.76.52:8080"))
	require.Equal(t, "https://edge.example", pickExchangeBaseURL("https://edge.example", ""))
}

func TestNormalizeAcceptsFluxClients(t *testing.T) {
	for _, id := range []string{
		"codex", "claude-code", "claude-desktop", "pi", "hermes", "openclaw",
		"paseo", "zcode", "deepseek-harness", "obsidian", "cherry-studio", "chatbox", "helper",
	} {
		got, err := normalizeOneClickClient(id)
		require.NoError(t, err, id)
		require.Equal(t, id, got)
	}
	// 大小写不影响
	got, err := normalizeOneClickClient("Hermes")
	require.NoError(t, err)
	require.Equal(t, "hermes", got)
}

func TestOneClickTicketRedisLifecycle(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	s := &OneClickService{Redis: rdb}
	_, err := s.Issue(context.Background(), 1, IssueOneClickInput{
		KeyID: 1, Client: "codex", OS: "macos", APIBase: "https://example.com",
	})
	var hx *httpx.Error
	require.ErrorAs(t, err, &hx)
	require.Equal(t, "ONECLICK_UNAVAILABLE", hx.Code)

	rec := OneClickTicketRecord{
		UserID: 9, KeyID: 42, Client: "chatbox", OS: "linux",
		APIBase: "https://example.com", IssuedAt: time.Now().Unix(),
	}
	raw, err := json.Marshal(rec)
	require.NoError(t, err)
	require.NoError(t, rdb.Set(context.Background(), oneClickTicketPrefix+"tok1", raw, oneClickTicketTTL).Err())

	got, err := rdb.GetDel(context.Background(), oneClickTicketPrefix+"tok1").Bytes()
	require.NoError(t, err)
	var decoded OneClickTicketRecord
	require.NoError(t, json.Unmarshal(got, &decoded))
	require.Equal(t, int64(42), decoded.KeyID)

	_, err = rdb.Get(context.Background(), oneClickTicketPrefix+"tok1").Result()
	require.Equal(t, redis.Nil, err)

	s2 := &OneClickService{Redis: rdb, Keys: &KeyService{}}
	_, err = s2.Exchange(context.Background(), "already-gone")
	require.ErrorAs(t, err, &hx)
	require.Equal(t, "TICKET_INVALID", hx.Code)
}

func TestHelperManifest(t *testing.T) {
	m := HelperManifest("0.1.0")
	require.Equal(t, "闲猫助手", m["name"])
	require.Equal(t, "xianmao", m["protocol"])
	dl, ok := m["downloads"].(map[string]any)
	require.True(t, ok)
	require.Contains(t, dl, "macos")
}

func TestHelperAuthStartAndPoll(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	s := &OneClickService{Redis: rdb, Keys: &KeyService{}}

	st, err := s.StartHelperAuth(context.Background(), "aabbccddeeff00112233445566778899", "http://127.0.0.1:8080", "macos")
	require.NoError(t, err)
	require.Equal(t, "pending", st.Status)

	polled, err := s.PollHelperAuth(context.Background(), "aabbccddeeff00112233445566778899")
	require.NoError(t, err)
	require.Equal(t, "pending", polled.Status)

	_, err = s.StartHelperAuth(context.Background(), "zz", "http://127.0.0.1:8080", "macos")
	var hx *httpx.Error
	require.ErrorAs(t, err, &hx)
	require.Equal(t, "INVALID_DEVICE_CODE", hx.Code)
}

func TestPreferredOneClickPlatform(t *testing.T) {
	require.Equal(t, "openai", preferredOneClickPlatform("codex"))
	require.Equal(t, "anthropic", preferredOneClickPlatform("obsidian"))
	require.Equal(t, "anthropic", preferredOneClickPlatform("claude-code"))
	require.Equal(t, "openai", preferredOneClickPlatform("cherry-studio"))
	require.Equal(t, "openai", preferredOneClickPlatform("opencode"))
	require.Equal(t, "gemini", preferredOneClickPlatform("gemini"))
	require.Equal(t, "openai", preferredOneClickPlatform("kimi-code"))
}

func TestListOneClickVendorsFiltersByAgent(t *testing.T) {
	gid := int64(1)
	pools := []scheduling.Pool{
		{Name: "g", Platform: "openai", Enabled: true, MarketPublished: true, Sub2APIGroupID: &gid},
		{Name: "a", Platform: "anthropic", Enabled: true, MarketPublished: true, Sub2APIGroupID: &gid},
		{Name: "m", Platform: "gemini", Enabled: true, MarketPublished: true, Sub2APIGroupID: &gid},
	}
	v, err := ListOneClickVendors("gemini", pools)
	require.NoError(t, err)
	require.Len(t, v, 1)
	require.Equal(t, "gemini", v[0].Platform)
	require.True(t, v[0].Default)

	v, err = ListOneClickVendors("opencode", pools)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(v), 2)
	var def string
	for _, o := range v {
		if o.Default {
			def = o.Platform
		}
	}
	require.Equal(t, "openai", def)
}

func TestPickGuaranteePoolPrefersPlatformThenComposite(t *testing.T) {
	gid := int64(1)
	pools := []scheduling.Pool{
		{Name: "a", Platform: "anthropic", Enabled: true, MarketPublished: true, Sub2APIGroupID: &gid},
		{Name: "all", Platform: "composite", Enabled: true, MarketPublished: true, Sub2APIGroupID: &gid},
		{Name: "g", Platform: "openai", Enabled: true, MarketPublished: true, Sub2APIGroupID: &gid},
	}
	p, err := pickGuaranteePool(pools, "openai")
	require.NoError(t, err)
	require.Equal(t, "g", p.Name)
	p, err = pickGuaranteePool(pools, "gemini")
	require.NoError(t, err)
	require.Equal(t, "all", p.Name)
}
