package routes

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/yx-token/token-market/market/enginehost"
)

func TestMarketEdgeDispatchRunsBeforeGatewayRoutes(t *testing.T) {
	t.Setenv("MARKET_EMBED", "1")
	t.Setenv("MARKET_EMBED_EDGE", "1")
	gin.SetMode(gin.TestMode)
	edgeCalls := 0
	gatewayCalls := 0
	edgeHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		edgeCalls++
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("edge:" + r.URL.Path))
	})
	router := gin.New()
	router.Use(marketEdgeDispatch(edgeHandler, enginehost.IsEdgeRequest))
	router.POST("/v1/messages", func(c *gin.Context) { gatewayCalls++; c.Status(http.StatusOK) })
	router.POST("/v1/chat/completions", func(c *gin.Context) { gatewayCalls++; c.Status(http.StatusOK) })
	router.POST("/v1beta/models/*action", func(c *gin.Context) { gatewayCalls++; c.Status(http.StatusOK) })
	router.GET("/v1/usage", func(c *gin.Context) { gatewayCalls++; c.Status(http.StatusOK) })
	router.GET("/antigravity/v1/usage", func(c *gin.Context) { gatewayCalls++; c.Status(http.StatusOK) })

	tests := []struct {
		name       string
		method     string
		target     string
		authorize  string
		wantStatus int
		wantEdge   bool
	}{
		{name: "anthropic buyer key", method: http.MethodPost, target: "/v1/messages", authorize: "Bearer tm-buyer", wantStatus: http.StatusAccepted, wantEdge: true},
		{name: "openai buyer key", method: http.MethodPost, target: "/v1/chat/completions", authorize: "Bearer tm-buyer", wantStatus: http.StatusAccepted, wantEdge: true},
		{name: "gemini query buyer key", method: http.MethodPost, target: "/v1beta/models/gemini:generateContent?key=tm-buyer", wantStatus: http.StatusAccepted, wantEdge: true},
		{name: "sub2api key", method: http.MethodPost, target: "/v1/chat/completions", authorize: "Bearer sk-sub2api", wantStatus: http.StatusOK},
		{name: "edge internal source key", method: http.MethodPost, target: "/v1/chat/completions", authorize: "Bearer tmik_internal", wantStatus: http.StatusOK},
		{name: "buyer local usage", method: http.MethodGet, target: "/v1/usage", authorize: "Bearer tm-buyer", wantStatus: http.StatusAccepted, wantEdge: true},
		{name: "buyer local usage alias", method: http.MethodGet, target: "/antigravity/v1/usage", authorize: "Bearer tm-buyer", wantStatus: http.StatusAccepted, wantEdge: true},
		{name: "sub2api native usage", method: http.MethodGet, target: "/v1/usage", authorize: "Bearer sk-sub2api", wantStatus: http.StatusOK},
		{name: "internal native usage", method: http.MethodGet, target: "/v1/usage", authorize: "Bearer tmik_internal", wantStatus: http.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.target, nil)
			if tc.authorize != "" {
				req.Header.Set("Authorization", tc.authorize)
			}
			rec := httptest.NewRecorder()
			edgeBefore, gatewayBefore := edgeCalls, gatewayCalls
			router.ServeHTTP(rec, req)
			require.Equal(t, tc.wantStatus, rec.Code)
			if tc.wantEdge {
				require.Equal(t, edgeBefore+1, edgeCalls)
				require.Equal(t, gatewayBefore, gatewayCalls)
			} else {
				require.Equal(t, edgeBefore, edgeCalls)
				require.Equal(t, gatewayBefore+1, gatewayCalls)
			}
		})
	}
}

func TestMarketEdgeDispatchRequiresTMKeyAtSupportedPath(t *testing.T) {
	t.Setenv("MARKET_EMBED", "1")
	t.Setenv("MARKET_EMBED_EDGE", "1")
	gin.SetMode(gin.TestMode)
	calls := 0
	router := gin.New()
	router.Use(marketEdgeDispatch(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusAccepted)
	}), enginehost.IsEdgeRequest))
	router.GET("/api/v1/admin/market/pools", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/market/pools", nil)
	req.Header.Set("Authorization", "Bearer tm-buyer")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Zero(t, calls)
}

func TestMarketEdgeMatcherCoversRegisteredGatewayRoutes(t *testing.T) {
	t.Setenv("MARKET_EMBED", "1")
	t.Setenv("MARKET_EMBED_EDGE", "1")
	router := newGatewayRoutesTestRouter()
	for _, route := range router.Routes() {
		// Host account balance is not a model API and must not expose the market's internal account.
		if route.Path == "/v1/sub2api/billing" {
			continue
		}
		t.Run(route.Method+" "+route.Path, func(t *testing.T) {
			parts := strings.Split(route.Path, "/")
			for i, part := range parts {
				if strings.HasPrefix(part, ":") {
					parts[i] = "example"
				} else if strings.HasPrefix(part, "*") {
					parts[i] = "example:generateContent"
				}
			}
			req := httptest.NewRequest(route.Method, strings.Join(parts, "/"), nil)
			req.Header.Set("Authorization", "Bearer tm-buyer")
			require.True(t, enginehost.IsEdgeRequest(req), "missing buyer Edge route")
		})
	}
}

func TestMarketEdgeDispatchPreservesDirectEdgeRejection(t *testing.T) {
	// enginehost owns a process-wide sync.Once runtime; isolate its lifecycle for repeatable tests.
	if os.Getenv("TM_EDGE_REJECTION_TEST_CHILD") != "1" {
		// Re-executes this test binary with a fixed -test.run pattern; no external input reaches the command.
		cmd := exec.Command(os.Args[0], "-test.run=^TestMarketEdgeDispatchPreservesDirectEdgeRejection$") //nolint:gosec // G702: re-exec of the current test binary
		cmd.Env = append(os.Environ(), "TM_EDGE_REJECTION_TEST_CHILD=1")
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", output)
		return
	}
	t.Setenv("MARKET_EMBED", "1")
	t.Setenv("MARKET_EMBED_EDGE", "1")
	t.Setenv("TM_ENV", "test")
	t.Setenv("TM_EDGE_ADDR", "127.0.0.1:0")
	t.Setenv("TM_EDGE_SPOOL_DIR", t.TempDir())
	t.Setenv("TM_DATABASE_DSN", "")
	t.Setenv("TM_CRYPTO_ACTIVE_KEY_ID", "k1")
	t.Setenv("TM_CRYPTO_KEYS", "k1:"+base64.StdEncoding.EncodeToString([]byte(strings.Repeat("e", 32))))
	mr := miniredis.RunT(t)
	// Use the wire format without importing market internals.
	require.NoError(t, mr.Set(fmt.Sprintf("tm:edge:key:%x", sha256.Sum256([]byte("tm-buyer"))), `{"buyer_key_id":41,"buyer_user_id":73,"status":"active","hops":[]}`))
	t.Setenv("TM_REDIS_ADDR", mr.Addr())
	t.Setenv("TM_REDIS_PASSWORD", "")
	t.Setenv("TM_REDIS_DB", "0")

	h := enginehost.EdgeRequestHandler()
	ctx, cancel := context.WithCancel(context.Background())
	done := enginehost.StartEdge(ctx)
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("Edge runtime did not stop")
		}
	})
	request := func(method, path string) *http.Request {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", "Bearer tm-buyer")
		return req
	}
	require.Eventually(t, func() bool {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, request(http.MethodPost, "/v1/images/generations"))
		return rec.Code == http.StatusNotFound
	}, 2*time.Second, time.Millisecond, "Edge must finish loading its cache")

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(marketEdgeDispatch(h, enginehost.IsEdgeRequest))
	gatewayCalls := 0
	for _, route := range []struct{ method, path string }{
		{http.MethodPost, "/v1/images/generations"},
		{http.MethodPost, "/v1/custom-voices"},
	} {
		router.Handle(route.method, route.path, func(c *gin.Context) {
			gatewayCalls++
			c.Status(http.StatusTeapot)
		})
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			direct := httptest.NewRecorder()
			h.ServeHTTP(direct, request(route.method, route.path))
			mounted := httptest.NewRecorder()
			router.ServeHTTP(mounted, request(route.method, route.path))
			require.Equal(t, http.StatusNotFound, direct.Code)
			require.Equal(t, direct.Code, mounted.Code)
			require.JSONEq(t, direct.Body.String(), mounted.Body.String())
			require.Contains(t, mounted.Body.String(), "unsupported endpoint")
		})
	}
	for _, path := range []string{"/v1/usage", "/antigravity/v1/usage"} {
		router.GET(path, func(c *gin.Context) { gatewayCalls++; c.Status(http.StatusTeapot) })
		t.Run("local balance "+path, func(t *testing.T) {
			direct := httptest.NewRecorder()
			h.ServeHTTP(direct, request(http.MethodGet, path))
			mounted := httptest.NewRecorder()
			router.ServeHTTP(mounted, request(http.MethodGet, path))
			require.Equal(t, http.StatusServiceUnavailable, direct.Code)
			require.Equal(t, direct.Code, mounted.Code)
			require.JSONEq(t, direct.Body.String(), mounted.Body.String())
			require.Contains(t, mounted.Body.String(), "usage balance unavailable", "a missing market DB must fail locally, never query internal credentials")
		})
	}
	require.Zero(t, gatewayCalls, "rejected buyer requests must never reach the original gateway")
}

func TestMarketEdgeDispatchInternalRequestDoesNotLoop(t *testing.T) {
	t.Setenv("MARKET_EMBED", "1")
	t.Setenv("MARKET_EMBED_EDGE", "1")
	gin.SetMode(gin.TestMode)
	router := gin.New()
	var hostURL string
	router.Use(marketEdgeDispatch(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, err := http.NewRequestWithContext(r.Context(), r.Method, hostURL+r.URL.Path, r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		req.Header.Set("Authorization", "Bearer tmik_internal")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer func() { _ = resp.Body.Close() }()
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}), enginehost.IsEdgeRequest))
	router.POST("/v1/messages", func(c *gin.Context) {
		require.Equal(t, "Bearer tmik_internal", c.GetHeader("Authorization"))
		c.String(http.StatusOK, "original gateway")
	})
	host := httptest.NewServer(router)
	t.Cleanup(host.Close)
	hostURL = host.URL

	req, err := http.NewRequest(http.MethodPost, host.URL+"/v1/messages", strings.NewReader(`{}`))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer tm-buyer")
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "original gateway", string(body))
}

func TestMarketEdgeDispatchPreservesStreamingAndRequestMetadata(t *testing.T) {
	t.Setenv("MARKET_EMBED", "1")
	t.Setenv("MARKET_EMBED_EDGE", "1")
	gin.SetMode(gin.TestMode)
	router := gin.New()
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	router.Use(marketEdgeDispatch(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "203.0.113.10", r.Header.Get("X-Forwarded-For"))
		_, ok := w.(http.Hijacker)
		require.True(t, ok, "WebSocket hijacking must remain available")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: first\n\n")
		flusher, ok := w.(http.Flusher)
		require.True(t, ok, "SSE flushing must remain available")
		flusher.Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}), enginehost.IsEdgeRequest))
	router.POST("/v1/messages", func(c *gin.Context) { c.Status(http.StatusTeapot) })
	host := httptest.NewServer(router)
	t.Cleanup(host.Close)
	req, err := http.NewRequest(http.MethodPost, host.URL+"/v1/messages", strings.NewReader(`{}`))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer tm-buyer")
	req.Header.Set("X-Forwarded-For", "203.0.113.10")
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "data: first\n", line, "first event must arrive before handler completes")
}
