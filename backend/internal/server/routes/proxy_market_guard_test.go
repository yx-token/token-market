package routes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/yx-token/token-market/internal/service"
	"github.com/yx-token/token-market/market/enginehost"
)

func TestProxyUpdateGuardAllowsNameAndBlocksDestructiveEdits(t *testing.T) {
	expires := time.Unix(2000000000, 0)
	current := &service.Proxy{ID: 7, Name: "before", Protocol: "http", Host: "proxy.local", Port: 8080, Username: "user", Password: "secret", Status: "active", ExpiresAt: &expires}
	for _, test := range []struct {
		name, body string
		blocked    bool
	}{
		{"name only", `{"name":"after"}`, false},
		{"same full endpoint", `{"name":"after","protocol":"http","host":"proxy.local","port":8080,"username":"user","password":"secret","status":"active","expires_at":2000000000}`, false},
		{"disable", `{"status":"inactive"}`, true},
		{"host", `{"host":"new.local"}`, true},
		{"protocol", `{"protocol":"socks5"}`, true},
		{"port", `{"port":9090}`, true},
		{"password", `{"password":"changed"}`, true},
		{"username", `{"username":"changed"}`, true},
		{"expiry", `{"expires_at":1000000000}`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			lockCalls, checkCalls, downstreamCalls := 0, 0, 0
			locked := false
			var check proxyMutationChecker
			lock := func(context.Context) (proxyMutationChecker, func(), error) {
				lockCalls++
				locked = true
				return check, func() { locked = false }, nil
			}
			get := func(context.Context, int64) (*service.Proxy, error) {
				require.True(t, locked, "current proxy must be read after acquiring lock")
				return current, nil
			}
			check = func(_ context.Context, ids []int64) error {
				require.True(t, locked, "binding check must run while holding lock")
				checkCalls++
				require.Equal(t, []int64{7}, ids)
				return &enginehost.APIError{Status: 409, Code: "MARKET_PROXY_IN_USE", Message: "market binding in use"}
			}
			router := gin.New()
			router.PUT("/proxies/:id", proxyMutationGuard(get, lock), func(c *gin.Context) {
				require.True(t, locked, "native write must run while holding lock")
				downstreamCalls++
				var decoded map[string]any
				require.NoError(t, c.ShouldBindJSON(&decoded), "guard must restore the exact body for native ShouldBindJSON")
				var want map[string]any
				require.NoError(t, json.Unmarshal([]byte(test.body), &want))
				require.Equal(t, want, decoded)
				c.Status(http.StatusOK)
			})
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/proxies/7", strings.NewReader(test.body)))
			require.Equal(t, 1, lockCalls)
			require.False(t, locked, "lock must release after handler or failed check")
			if test.blocked {
				require.Equal(t, 409, recorder.Code)
				require.Contains(t, recorder.Body.String(), "MARKET_PROXY_IN_USE")
				require.Equal(t, 1, checkCalls)
				require.Zero(t, downstreamCalls)
			} else {
				require.Equal(t, http.StatusOK, recorder.Code)
				require.Zero(t, checkCalls)
				require.Equal(t, 1, downstreamCalls)
			}
		})
	}
}

func TestProxyDeleteAndBatchGuardsHoldLockThroughNativeHandler(t *testing.T) {
	for _, method := range []string{http.MethodDelete, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
			locked, released := false, false
			var check proxyMutationChecker
			lock := func(context.Context) (proxyMutationChecker, func(), error) {
				locked = true
				return check, func() { locked = false; released = true }, nil
			}
			check = func(_ context.Context, ids []int64) error {
				require.Equal(t, []int64{7}, ids)
				return nil
			}
			router := gin.New()
			getCalls := 0
			get := func(context.Context, int64) (*service.Proxy, error) { getCalls++; return &service.Proxy{ID: 7}, nil }
			downstream := func(c *gin.Context) {
				require.True(t, locked)
				require.False(t, released)
				if method == http.MethodPost {
					var input struct {
						IDs []int64 `json:"ids"`
					}
					require.NoError(t, c.ShouldBindJSON(&input))
					require.Equal(t, []int64{7}, input.IDs)
				}
				c.Status(http.StatusOK)
			}
			path := "/proxies/7"
			if method == http.MethodPost {
				path = "/proxies/batch-delete"
				router.POST(path, proxyBatchMutationGuard(lock), downstream)
			} else {
				router.DELETE("/proxies/:id", guardProxyIDs(get, lock), downstream)
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(method, path, strings.NewReader(`{"ids":[7]}`)))
			require.Equal(t, http.StatusOK, recorder.Code)
			require.False(t, locked)
			require.True(t, released)
			if method == http.MethodDelete {
				require.Equal(t, 1, getCalls)
			}
		})
	}
}

func TestProxyImportGuardLocksBeforeListAndHoldsThroughWrite(t *testing.T) {
	current := service.Proxy{ID: 7, Protocol: "http", Host: "proxy.local", Port: 8080, Username: "user", Password: "secret", Status: "active"}
	for _, path := range []string{"/proxies/data", "/accounts/data"} {
		for _, test := range []struct {
			status  string
			blocked bool
		}{
			{"active", false}, {"inactive", true}, {"expired", true},
		} {
			t.Run(path+test.status, func(t *testing.T) {
				body := `{"data":{"proxies":[{"proxy_key":"http|proxy.local|8080|user|secret","protocol":"http","host":"proxy.local","port":8080,"username":"user","password":"secret","status":"` + test.status + `"}]}}`
				locked, released, listCalls, checkCalls, downstreamCalls := false, false, 0, 0, 0
				var check proxyMutationChecker
				lock := func(context.Context) (proxyMutationChecker, func(), error) {
					locked = true
					return check, func() { locked = false; released = true }, nil
				}
				list := func(context.Context) ([]service.Proxy, error) {
					require.True(t, locked, "inventory read must happen after lock")
					listCalls++
					return []service.Proxy{current}, nil
				}
				check = func(_ context.Context, ids []int64) error {
					require.True(t, locked)
					require.Equal(t, []int64{7}, ids)
					checkCalls++
					return &enginehost.APIError{Status: 409, Code: "MARKET_PROXY_IN_USE", Message: "market binding in use"}
				}
				router := gin.New()
				router.POST(path, guardProxyImport(lock, list), func(c *gin.Context) {
					require.True(t, locked)
					downstreamCalls++
					var value map[string]any
					require.NoError(t, c.ShouldBindJSON(&value))
					c.Status(http.StatusOK)
				})
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
				require.Equal(t, 1, listCalls)
				require.True(t, released)
				if test.blocked {
					require.Equal(t, 409, recorder.Code)
					require.Equal(t, 1, checkCalls)
					require.Zero(t, downstreamCalls)
				} else {
					require.Equal(t, http.StatusOK, recorder.Code)
					require.Zero(t, checkCalls)
					require.Equal(t, 1, downstreamCalls)
				}
			})
		}
	}
}

func TestProxyUpdateMutationLockBlocksConcurrentMarketAllocation(t *testing.T) {
	var stateMu sync.Mutex
	mutationLock := make(chan struct{}, 1)
	mutationLock <- struct{}{}
	proxyEndpoint := "old.example"
	var check proxyMutationChecker
	lock := func(context.Context) (proxyMutationChecker, func(), error) {
		<-mutationLock
		return check, func() { mutationLock <- struct{}{} }, nil
	}
	get := func(context.Context, int64) (*service.Proxy, error) {
		stateMu.Lock()
		defer stateMu.Unlock()
		return &service.Proxy{ID: 7, Protocol: "http", Host: proxyEndpoint, Port: 8080, Status: "active"}, nil
	}
	check = func(context.Context, []int64) error { return nil }
	updateEntered := make(chan struct{})
	finishUpdate := make(chan struct{})
	allocationDone := make(chan string, 1)
	router := gin.New()
	router.PUT("/proxies/:id", proxyMutationGuard(get, lock), func(c *gin.Context) {
		stateMu.Lock()
		proxyEndpoint = "new.example"
		stateMu.Unlock()
		close(updateEntered)
		<-finishUpdate
		c.Status(http.StatusOK)
	})
	updateDone := make(chan struct{})
	go func() {
		defer close(updateDone)
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPut, "/proxies/7", strings.NewReader(`{"host":"new.example"}`)))
	}()
	<-updateEntered
	go func() {
		<-mutationLock
		stateMu.Lock()
		allocationDone <- proxyEndpoint
		stateMu.Unlock()
		mutationLock <- struct{}{}
	}()
	select {
	case endpoint := <-allocationDone:
		t.Fatalf("market allocation crossed the native write lock and observed %q before handler completion", endpoint)
	case <-time.After(30 * time.Millisecond):
	}
	close(finishUpdate)
	<-updateDone
	select {
	case endpoint := <-allocationDone:
		require.Equal(t, "new.example", endpoint)
	case <-time.After(time.Second):
		t.Fatal("market allocation did not resume after proxy mutation completed")
	}
}
