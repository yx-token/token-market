//go:build unit

package zhipu

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRemoteConfigHourlyCache(t *testing.T) {
	var requests atomic.Int64
	var failed atomic.Bool
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("X-Api-Key") != "key-id.secret" || r.Header.Get("Authorization") != "" || r.Header.Get("X-Config-Source") != "runtime" {
			t.Error("config credential/source headers missing")
		}
		if r.URL.Path != "/api/v1/agent/configs" || r.URL.Query().Get("app_version") != ClientVersion || r.URL.Query().Get("platform") == "" {
			t.Error("bad config request")
		}
		if failed.Load() {
			w.WriteHeader(503)
			return
		}
		respond(t, w, 0, map[string]any{"codingPlanSignature": map[string]bool{"enable": true}, "proxyEndpoint": map[string]any{"mapping": []map[string]string{{"from": "https://open.bigmodel.cn/api/anthropic/v1/messages", "to": "https://zcode.z.ai/api/v1/ultra/anthropic/v1/messages"}}}})
	})
	cache := NewRemoteConfigCache(c, RemoteConfigOptions{Headers: http.Header{"X-Config-Source": {"runtime"}, "authorization": {"must-not-be-sent"}, "x-api-key": {"must-be-overridden"}}})
	now := time.Unix(2000000000, 0)
	cache.now = func() time.Time { return now }
	errorsCh := make(chan error, 12)
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cfg, err := cache.Get(context.Background(), "key-id.secret")
			if err == nil && (cfg.CodingPlanSignature.Enable == nil || !*cfg.CodingPlanSignature.Enable || len(cfg.ProxyEndpoint.Mapping) != 1) {
				err = errors.New("bad cached config")
			}
			errorsCh <- err
		}()
	}
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	if requests.Load() != 1 {
		t.Fatalf("cache duplicated request: %d", requests.Load())
	}
	cfg, err := cache.Get(context.Background(), "key-id.secret")
	if err != nil {
		t.Fatal(err)
	}
	*cfg.CodingPlanSignature.Enable = false
	cfg.ProxyEndpoint.Mapping[0].To = "mutated"
	cfg, err = cache.Get(context.Background(), "key-id.secret")
	if err != nil {
		t.Fatal(err)
	}
	if !*cfg.CodingPlanSignature.Enable || cfg.ProxyEndpoint.Mapping[0].To == "mutated" {
		t.Fatal("cache snapshot shares mutable data")
	}
	now = now.Add(time.Hour)
	if _, err := cache.Get(context.Background(), "key-id.secret"); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 {
		t.Fatal("cache did not refresh hourly")
	}
	failed.Store(true)
	now = now.Add(time.Hour)
	stale, err := cache.Get(context.Background(), "key-id.secret")
	if err == nil || stale.CodingPlanSignature.Enable != nil {
		t.Fatal("failed refresh retained stale feature enable")
	}
	if _, err := cache.Get(context.Background(), "key-id.secret"); err == nil {
		t.Fatal("failed refresh result not cached")
	}
	if requests.Load() != 3 {
		t.Fatal("failed config refresh retried too often")
	}
	failed.Store(false)
	now = now.Add(time.Minute)
	if _, err := cache.Get(context.Background(), "key-id.secret"); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 4 {
		t.Fatal("failure backoff incorrectly lasts an hour")
	}
}

func TestAutoSignatureMode(t *testing.T) {
	for _, scenario := range []string{"enabled", "disabled", "unavailable", "missing"} {
		t.Run(scenario, func(t *testing.T) {
			remote, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if scenario == "unavailable" {
					w.WriteHeader(503)
					return
				}
				config := map[string]any{}
				if scenario != "missing" {
					config["codingPlanSignature"] = map[string]bool{"enable": scenario == "enabled"}
				}
				respond(t, w, 0, config)
			})
			c, server, f := newSigningServer(t)
			s := testSigner(t, c, server.URL, SignatureAuto, NewRemoteConfigCache(remote))
			req := signingRequest(t, server)
			err := s.Sign(context.Background(), req, "key-id.secret", "end-user-session")
			if err != nil {
				t.Fatal(err)
			}
			if (req.Header.Get("X-Client-Sig") != "") != (scenario == "enabled") {
				t.Fatal("auto mode ignored remote switch")
			}
			if scenario != "enabled" && f.handshakes.Load() != 0 {
				t.Fatal("disabled/failed gate performed handshake")
			}
		})
	}
}

func TestEndpointMapping(t *testing.T) {
	var config AgentConfig
	config.ProxyEndpoint.Mapping = []EndpointMapping{{"https://open.bigmodel.cn/api/anthropic/v1/messages", "https://zcode.z.ai/api/v1/ultra/anthropic/v1/messages"}}
	original, err := url.Parse("https://open.bigmodel.cn/api/anthropic/v1/messages?foo=bar")
	if err != nil {
		t.Fatal(err)
	}
	mapped, err := config.MapEndpoint(original)
	if err != nil {
		t.Fatal(err)
	}
	if mapped.String() != "https://zcode.z.ai/api/v1/ultra/anthropic/v1/messages?foo=bar" || original.Host != "open.bigmodel.cn" {
		t.Fatal("mapping lost query or mutated original")
	}
	config.ProxyEndpoint.Mapping[0].To = "http://unsafe.invalid/path"
	if _, err := config.MapEndpoint(original); err == nil {
		t.Fatal("unsafe mapping accepted")
	}
}

func TestSourceHeaders(t *testing.T) {
	h := SourceHeaders(SourceOptions{Version: "3.11.2", Language: "zh-CN", Timezone: "Asia/Singapore", Platform: "linux-x64", OSCategory: "linux", OSVersion: "6.1", DeviceMID: "device"})
	for name, want := range map[string]string{"User-Agent": "ZCode/3.11.2", "X-ZCode-App-Version": "3.11.2", "X-ZCode-Agent": "glm", "X-Title": "Z Code@cli", "X-Release-Channel": "production", "X-Client-Language": "zh-CN", "X-Client-Timezone": "Asia/Singapore", "X-Platform": "linux-x64", "X-Os-Category": "linux", "X-Os-Version": "6.1", "HTTP-Referer": ZCodeOrigin, "X-Device-Mid": "device"} {
		if h.Get(name) != want {
			t.Errorf("%s = %q, want %q", name, h.Get(name), want)
		}
	}
}
