package edgeloop

import (
	"context"
	"encoding/base64"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/yx-token/token-market/market/internal/platform/config"
)

func TestRuntimeSharesHandlerBeforeAndAfterStartup(t *testing.T) {
	mr := miniredis.RunT(t)
	cfg := &config.Config{
		Redis:   config.RedisConfig{Addr: mr.Addr()},
		Crypto:  config.CryptoConfig{ActiveKeyID: "k1", Keys: map[string]string{"k1": base64.StdEncoding.EncodeToString([]byte(strings.Repeat("e", 32)))}},
		Sub2API: config.Sub2APIConfig{GatewayURL: "http://127.0.0.1:1"},
		Edge: config.EdgeConfig{
			Addr: "127.0.0.1:0", SpoolDir: t.TempDir(), MaxBodyBytes: 1 << 20,
			ConnectTimeout: time.Second, FirstByteTimeout: time.Second,
		},
	}
	runtime, err := NewRuntime(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	handler := runtime.Handler()
	if handler != runtime.Handler() {
		t.Fatal("runtime must expose the same handler instance")
	}
	request := func() int {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader("{}")))
		if rec.Code == http.StatusServiceUnavailable && (rec.Header().Get("Content-Type") != "application/json" || !strings.Contains(rec.Body.String(), `"type":"api_error"`)) {
			t.Errorf("unavailable response is not an Edge JSON error: %s", rec.Body.String())
		}
		return rec.Code
	}
	if got := request(); got != http.StatusServiceUnavailable {
		t.Fatalf("cold handler status = %d, want 503", got)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- runtime.Run(ctx) }()
	waitRuntimeActive(t, runtime)
	if got := request(); got != http.StatusUnauthorized {
		t.Fatalf("started handler status = %d, want 401", got)
	}
	if err := runtime.Run(ctx); err == nil {
		t.Fatal("runtime must reject a second startup")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runtime did not shut down")
	}
	if got := request(); got != http.StatusServiceUnavailable {
		t.Fatalf("stopped handler status = %d, want 503", got)
	}
}

func TestRuntimeKeepsMountedHandlerWhenPublicPortIsOccupied(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = occupied.Close() })
	mr := miniredis.RunT(t)
	cfg := &config.Config{
		Redis:   config.RedisConfig{Addr: mr.Addr()},
		Crypto:  config.CryptoConfig{ActiveKeyID: "k1", Keys: map[string]string{"k1": base64.StdEncoding.EncodeToString([]byte(strings.Repeat("e", 32)))}},
		Sub2API: config.Sub2APIConfig{GatewayURL: "http://127.0.0.1:1"},
		Edge: config.EdgeConfig{
			Addr: occupied.Addr().String(), SpoolDir: t.TempDir(), MaxBodyBytes: 1 << 20,
			ConnectTimeout: time.Second, FirstByteTimeout: time.Second,
		},
	}
	failureLogged := make(listenerFailureSignal, 1)
	runtime, err := NewRuntime(cfg, slog.New(slog.NewTextHandler(failureLogged, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runtime.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("runtime shutdown: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("runtime did not stop after cancellation")
		}
	})
	select {
	case <-failureLogged:
	case <-time.After(2 * time.Second):
		t.Fatal("occupied listener did not report a bind failure")
	}
	select {
	case err := <-done:
		t.Fatalf("public listener failure stopped the shared runtime: %v", err)
	default:
	}
	host := httptest.NewServer(runtime.Handler())
	t.Cleanup(host.Close)
	resp, err := host.Client().Post(host.URL+"/v1/messages", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("mounted handler status = %d, want 401 after listener failure", resp.StatusCode)
	}
}

type listenerFailureSignal chan struct{}

func (signal listenerFailureSignal) Write(data []byte) (int, error) {
	if strings.Contains(string(data), "edge public listener failed") {
		select {
		case signal <- struct{}{}:
		default:
		}
	}
	return len(data), nil
}

func TestRuntimeDrainsMountedHandlerOnShutdown(t *testing.T) {
	mr := miniredis.RunT(t)
	cfg := &config.Config{
		Redis:   config.RedisConfig{Addr: mr.Addr()},
		Crypto:  config.CryptoConfig{ActiveKeyID: "k1", Keys: map[string]string{"k1": base64.StdEncoding.EncodeToString([]byte(strings.Repeat("e", 32)))}},
		Sub2API: config.Sub2APIConfig{GatewayURL: "http://127.0.0.1:1"},
		Edge: config.EdgeConfig{
			Addr: "127.0.0.1:0", SpoolDir: t.TempDir(), MaxBodyBytes: 1 << 20,
			ConnectTimeout: time.Second, FirstByteTimeout: time.Second,
		},
	}
	runtime, err := NewRuntime(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	runtime.handler.handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusNoContent)
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runtime.Run(ctx) }()
	waitRuntimeActive(t, runtime)

	handler := runtime.Handler()
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/messages", nil))
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("mounted handler did not receive request")
	}

	cancel()
	select {
	case err := <-done:
		t.Fatalf("runtime exited before in-flight request drained: %v", err)
	case <-time.After(30 * time.Millisecond):
	}

	newRequest := httptest.NewRecorder()
	handler.ServeHTTP(newRequest, httptest.NewRequest(http.MethodPost, "/v1/messages", nil))
	if newRequest.Code != http.StatusServiceUnavailable {
		t.Fatalf("request after shutdown began got %d, want 503", newRequest.Code)
	}

	close(release)
	select {
	case <-requestDone:
	case <-time.After(2 * time.Second):
		t.Fatal("in-flight mounted request did not finish")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runtime did not finish after request drained")
	}
}

func TestHandlerGatePreservesFlusher(t *testing.T) {
	gate := &handlerGate{drained: closedSignal()}
	gate.handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("mounted handler lost flusher support")
			return
		}
		_, _ = io.WriteString(w, "data: event\n\n")
		flusher.Flush()
	})
	gate.activate()
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/responses", nil))
	if !rec.Flushed {
		t.Fatal("mounted handler did not flush stream")
	}
}

func TestHandlerGateClosesHijackedConnectionOnShutdown(t *testing.T) {
	gate := &handlerGate{drained: closedSignal()}
	started := make(chan struct{})
	done := make(chan struct{})
	gate.handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		defer close(done)
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Error("mounted handler lost hijacker support")
			return
		}
		conn, _, err := hijacker.Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = conn.Close() }()
		close(started)
		_, _ = io.Copy(io.Discard, conn)
	})
	gate.activate()
	server := httptest.NewServer(gate)
	t.Cleanup(server.Close)
	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_, _ = io.WriteString(conn, "GET /v1/responses HTTP/1.1\r\nHost: example.test\r\n\r\n")
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not hijack connection")
	}
	drained := gate.deactivate()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not close hijacked connection")
	}
	select {
	case <-drained:
	case <-time.After(2 * time.Second):
		t.Fatal("hijacked request did not drain")
	}
}

func TestHandlerGateCancelsRequestsAfterGracePeriod(t *testing.T) {
	started := make(chan struct{})
	done := make(chan struct{})
	gate := &handlerGate{drained: closedSignal()}
	gate.handler = http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	})
	gate.activate()
	go func() {
		defer close(done)
		gate.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/messages", nil))
	}()
	<-started
	drained := gate.deactivate()
	gate.cancelRequests()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("request context did not cancel")
	}
	select {
	case <-drained:
	case <-time.After(2 * time.Second):
		t.Fatal("canceled request did not drain")
	}
}

func TestHandlerGateCancellationInterruptsSlowUpload(t *testing.T) {
	started := make(chan struct{})
	gate := &handlerGate{drained: closedSignal()}
	gate.handler = http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		_, _ = io.Copy(io.Discard, r.Body)
	})
	gate.activate()
	server := httptest.NewServer(gate)
	t.Cleanup(server.Close)
	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_, err = io.WriteString(conn, "POST /v1/messages HTTP/1.1\r\nHost: example.test\r\nContent-Length: 100\r\n\r\nx")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("upload did not reach handler")
	}
	drained := gate.deactivate()
	canceled := make(chan struct{})
	go func() {
		defer close(canceled)
		gate.cancelRequests()
	}()
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("slow upload blocked shutdown cancellation")
	}
	select {
	case <-drained:
	case <-time.After(time.Second):
		t.Fatal("slow upload did not drain")
	}
}

func TestHandlerGateCancelsRequestsOutsideGateLock(t *testing.T) {
	gate := &handlerGate{drained: closedSignal()}
	bodyClosed := false
	gate.requests = map[*requestState]struct{}{
		{
			cancel: func() {},
			writer: httptest.NewRecorder(),
			body: closeFuncBody{close: func() error {
				gate.mu.Lock()
				defer gate.mu.Unlock()
				bodyClosed = true
				return nil
			}},
		}: {},
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		gate.cancelRequests()
	}()
	select {
	case <-done:
		if !bodyClosed {
			t.Fatal("request body did not close")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelRequests held the gate lock while closing a request body")
	}
}

func TestHandlerGateSkipsFinishedRequests(t *testing.T) {
	gate := &handlerGate{drained: closedSignal()}
	gate.requests = map[*requestState]struct{}{
		{
			finished: true,
			cancel:   func() { t.Error("finished request canceled") },
			writer:   httptest.NewRecorder(),
			body: closeFuncBody{close: func() error {
				t.Error("finished request body closed")
				return nil
			}},
		}: {},
	}
	gate.cancelRequests()
}

func TestHandlerGateWaitsForCancelOperationsBeforeReturning(t *testing.T) {
	started := make(chan struct{})
	bodyClosing := make(chan struct{})
	releaseClose := make(chan struct{})
	requestDone := make(chan struct{})
	cancelDone := make(chan struct{})
	gate := &handlerGate{drained: closedSignal()}
	gate.handler = http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	})
	gate.activate()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	req.Body = closeFuncBody{close: func() error {
		close(bodyClosing)
		<-releaseClose
		return nil
	}}
	go func() {
		defer close(requestDone)
		gate.ServeHTTP(httptest.NewRecorder(), req)
	}()
	<-started
	gate.deactivate()
	go func() {
		defer close(cancelDone)
		gate.cancelRequests()
	}()
	<-bodyClosing
	select {
	case <-requestDone:
		t.Error("handler returned while cancellation still held its writer")
	case <-time.After(20 * time.Millisecond):
	}
	close(releaseClose)
	select {
	case <-requestDone:
	case <-time.After(time.Second):
		t.Fatal("handler did not return after cancellation finished")
	}
	<-cancelDone
}

type closeFuncBody struct{ close func() error }

func (closeFuncBody) Read([]byte) (int, error) { return 0, io.EOF }
func (b closeFuncBody) Close() error           { return b.close() }

func TestShutdownServersUsesSharedDeadline(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		started <- struct{}{}
		<-release
	})
	public := httptest.NewServer(handler)
	admin := httptest.NewServer(handler)
	t.Cleanup(func() {
		close(release)
		public.Close()
		admin.Close()
	})
	for _, target := range []string{public.URL, admin.URL} {
		go func() {
			resp, err := http.Get(target)
			if err == nil {
				_ = resp.Body.Close()
			}
		}()
	}
	for range 2 {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("server did not receive blocking request")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		shutdownServers(ctx, public.Config, admin.Config)
	}()
	select {
	case <-done:
		if ctx.Err() == nil {
			t.Fatal("shutdown returned before the shared deadline while handlers were active")
		}
	case <-time.After(time.Second):
		t.Fatal("server shutdown did not respect the shared deadline")
	}
}

func waitRuntimeActive(t *testing.T, runtime *Runtime) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		runtime.handler.mu.Lock()
		active := runtime.handler.active
		runtime.handler.mu.Unlock()
		if active {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("runtime did not become active")
}
