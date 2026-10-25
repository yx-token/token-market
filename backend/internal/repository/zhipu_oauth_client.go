package repository

import (
	"net/http"

	"github.com/yx-token/token-market/internal/config"
	"github.com/yx-token/token-market/internal/pkg/zhipu"
	"github.com/yx-token/token-market/internal/service"
)

type zhipuAccountHTTPClient struct {
	upstream    service.HTTPUpstream
	proxy       string
	id          int64
	concurrency int
}

func (c zhipuAccountHTTPClient) Do(req *http.Request) (*http.Response, error) {
	ctx := service.WithHTTPUpstreamRedirectsDisabled(req.Context())
	ctx = service.WithHTTPUpstreamPublicHostsOnly(ctx)
	return c.upstream.Do(req.WithContext(ctx), c.proxy, c.id, max(c.concurrency, 1))
}

func NewZhipuClientFactory(upstream service.HTTPUpstream, cfg *config.Config) service.ZhipuClientFactory {
	return func(proxy string, id int64, concurrency int) (*zhipu.Client, error) {
		return zhipu.NewClient(zhipu.Options{HTTPClient: zhipuAccountHTTPClient{upstream: upstream, proxy: proxy, id: id, concurrency: concurrency}, Version: cfg.Gateway.Zhipu.ClientVersion, Source: zhipu.SourceOptions{Timezone: cfg.Timezone}})
	}
}
