//go:build unit

package repository

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/yx-token/token-market/internal/config"
	"github.com/yx-token/token-market/internal/pkg/zhipu"
	"github.com/yx-token/token-market/internal/service"
)

type zhipuFactoryUpstreamTest struct {
	service.HTTPUpstream
	t      *testing.T
	called bool
}

func (u *zhipuFactoryUpstreamTest) Do(r *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
	u.called = true
	require.Equal(u.t, "http://account-proxy:7890", proxy)
	require.EqualValues(u.t, 7, id)
	require.Equal(u.t, 3, concurrency)
	require.True(u.t, service.HTTPUpstreamRedirectsDisabled(r.Context()))
	require.True(u.t, service.HTTPUpstreamPublicHostsOnly(r.Context()))
	require.Equal(u.t, "ZCode/3.11.2", r.Header.Get("User-Agent"))
	require.Equal(u.t, "Asia/Singapore", r.Header.Get("X-Client-Timezone"))
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"code":0,"data":{"status":"pending"}}`))}, nil
}

func TestZhipuClientFactoryKeepsProxyAndEgressPolicy(t *testing.T) {
	cfg := &config.Config{Timezone: "Asia/Singapore"}
	cfg.Gateway.Zhipu.ClientVersion = zhipu.ClientVersion
	u := &zhipuFactoryUpstreamTest{t: t}
	factory := NewZhipuClientFactory(u, cfg)
	client, err := factory("http://account-proxy:7890", 7, 3)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := client.Poll(ctx, zhipu.ProviderBigModel, "flow", "poll")
	require.NoError(t, err)
	require.Equal(t, zhipu.PollPending, result.Status)
	require.True(t, u.called)
}
