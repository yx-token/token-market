// Package edgeloop 市场 Edge 服务循环：可被独立 market-edge 或宿主进程（MARKET_EMBED）调用。
package edgeloop

import (
	"bufio"
	"context"
	"errors"
	"expvar"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/yx-token/token-market/market/internal/edge"
	"github.com/yx-token/token-market/market/internal/edgecfg"
	"github.com/yx-token/token-market/market/internal/platform/buildinfo"
	"github.com/yx-token/token-market/market/internal/platform/config"
	"github.com/yx-token/token-market/market/internal/platform/crypto"
)

// Runtime 持有单份 Edge 服务状态，可同时挂载为宿主 handler 并监听独立公网口。
// 构造后 Handler 可立即挂载；Run 完成 Redis 冷启动后才接受请求。
type Runtime struct {
	cfg        *config.Config
	log        *slog.Logger
	cache      *edge.Cache
	attempts   *edge.AttemptLogger
	srv        *edge.Server
	handler    *handlerGate
	rdb        *redis.Client
	pool       *pgxpool.Pool
	gatewayURL string

	runMu sync.Mutex
	run   bool
}

// NewRuntime 创建 Edge 状态但不监听端口。调用方可先通过 Handler 挂载同一实例，
// 再调用 Run 启动缓存刷新、尝试日志和可选独立监听口。
func NewRuntime(cfg *config.Config, log *slog.Logger) (*Runtime, error) {
	if log == nil {
		log = slog.Default()
	}
	if cfg == nil {
		return nil, errors.New("edge config is required")
	}
	if cfg.Sub2API.GatewayURL == "" || cfg.Redis.Addr == "" {
		return nil, errors.New("sub2api.gateway_url (or base_url) and redis.addr are required")
	}
	gateway, err := url.Parse(cfg.Sub2API.GatewayURL)
	if err != nil {
		return nil, err
	}
	kr, err := crypto.NewKeyring(cfg.Crypto)
	if err != nil {
		return nil, err
	}

	rdb := redis.NewClient(&redis.Options{Addr: cfg.Redis.Addr, Password: cfg.Redis.Password, DB: cfg.Redis.DB})
	cache := edge.NewCache(edgecfg.Store{Redis: rdb}, log)
	var pool *pgxpool.Pool
	if cfg.Database.DSN != "" {
		pool, err = newLazyPool(cfg.Database.DSN, cfg.Database.MaxConns, cfg.Database.TimeZone)
		if err != nil {
			log.Warn("edge database unavailable, attempts will be spooled", "err", err)
			pool = nil
		}
	}
	attempts := edge.NewAttemptLogger(pool, cfg.Edge.SpoolDir, log)
	var credits edge.CreditSelector
	if cfg.Database.DSN != "" {
		credits = &edge.DBCreditSelector{DB: pool, Keyring: kr}
	}
	srv := edge.NewServer(edge.Config{
		GatewayURL:       gateway,
		ConnectTimeout:   cfg.Edge.ConnectTimeout,
		FirstByteTimeout: cfg.Edge.FirstByteTimeout,
		MaxBodyBytes:     cfg.Edge.MaxBodyBytes,
		BreakerFailures:  cfg.Edge.BreakerFailures,
		BreakerCooldown:  cfg.Edge.BreakerCooldown,
		TrustedProxies:   cfg.Edge.TrustedProxies,
		InvalidKeyLimit:  cfg.Edge.InvalidKeyLimit,
	}, cache, kr, attempts, log)
	srv.SetCreditSelector(credits)
	srv.SetUsageReader(edge.NewDBUsageReader(pool))
	gate := &handlerGate{handler: srv, drained: closedSignal()}
	return &Runtime{
		cfg: cfg, log: log, cache: cache, attempts: attempts, srv: srv, handler: gate,
		rdb: rdb, pool: pool, gatewayURL: gateway.String(),
	}, nil
}

type handlerGate struct {
	mu            sync.Mutex
	handler       http.Handler
	active        bool
	inFlight      int
	drained       chan struct{}
	hijackedConns map[net.Conn]struct{}
	requests      map[*requestState]struct{}
}

type requestState struct {
	mu       sync.Mutex
	finished bool
	cancel   context.CancelFunc
	body     io.ReadCloser
	writer   http.ResponseWriter
}

func closedSignal() chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}

func (g *handlerGate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	state := &requestState{cancel: cancel, body: r.Body, writer: w}
	g.mu.Lock()
	if !g.active {
		g.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"error":{"type":"api_error","message":"market edge unavailable"}}`+"\n")
		return
	}
	if g.inFlight == 0 {
		g.drained = make(chan struct{})
	}
	g.inFlight++
	if g.requests == nil {
		g.requests = make(map[*requestState]struct{})
	}
	g.requests[state] = struct{}{}
	g.mu.Unlock()
	tracked := &trackedWriter{ResponseWriter: w, gate: g}
	defer func() {
		state.mu.Lock()
		state.finished = true
		state.mu.Unlock()
		g.mu.Lock()
		g.inFlight--
		delete(g.requests, state)
		if tracked.conn != nil {
			delete(g.hijackedConns, tracked.conn)
		}
		if g.inFlight == 0 {
			close(g.drained)
		}
		g.mu.Unlock()
	}()
	flusher, canFlush := w.(http.Flusher)
	hijacker, canHijack := w.(http.Hijacker)
	r = r.WithContext(ctx)
	switch {
	case canFlush && canHijack:
		g.handler.ServeHTTP(trackedFlusherHijacker{trackedHijacker{tracked, hijacker}, flusher}, r)
	case canFlush:
		g.handler.ServeHTTP(trackedFlusher{tracked, flusher}, r)
	case canHijack:
		g.handler.ServeHTTP(trackedHijacker{tracked, hijacker}, r)
	default:
		g.handler.ServeHTTP(tracked, r)
	}
}

func (g *handlerGate) cancelRequests() {
	g.mu.Lock()
	requests := make([]*requestState, 0, len(g.requests))
	for request := range g.requests {
		requests = append(requests, request)
	}
	g.mu.Unlock()
	for _, request := range requests {
		request.mu.Lock()
		if request.finished {
			request.mu.Unlock()
			continue
		}
		// Hold the request lock until external operations finish so the host cannot reuse its writer.
		request.cancel()
		controller := http.NewResponseController(request.writer)
		_ = controller.SetWriteDeadline(time.Now())
		_ = controller.SetReadDeadline(time.Now())
		if request.body != nil {
			_ = request.body.Close()
		}
		request.mu.Unlock()
	}
}

func (g *handlerGate) activate() {
	g.mu.Lock()
	g.active = true
	g.mu.Unlock()
}

func (g *handlerGate) deactivate() <-chan struct{} {
	g.mu.Lock()
	g.active = false
	connections := make([]net.Conn, 0, len(g.hijackedConns))
	for conn := range g.hijackedConns {
		connections = append(connections, conn)
	}
	g.mu.Unlock()
	for _, conn := range connections {
		_ = conn.Close()
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.inFlight == 0 {
		return closedSignal()
	}
	return g.drained
}

type trackedWriter struct {
	http.ResponseWriter
	gate *handlerGate
	conn net.Conn
}

func (w *trackedWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

type trackedHijacker struct {
	*trackedWriter
	hijacker http.Hijacker
}

func (w trackedHijacker) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := w.hijacker.Hijack()
	if err != nil {
		return nil, nil, err
	}
	w.gate.mu.Lock()
	if w.gate.hijackedConns == nil {
		w.gate.hijackedConns = make(map[net.Conn]struct{})
	}
	if !w.gate.active {
		w.gate.mu.Unlock()
		_ = conn.Close()
		return nil, nil, errors.New("edge runtime is shutting down")
	}
	w.gate.hijackedConns[conn] = struct{}{}
	w.conn = conn
	w.gate.mu.Unlock()
	return conn, rw, nil
}

type trackedFlusher struct {
	*trackedWriter
	flusher http.Flusher
}

func (w trackedFlusher) Flush() { w.flusher.Flush() }

type trackedFlusherHijacker struct {
	trackedHijacker
	flusher http.Flusher
}

func (w trackedFlusherHijacker) Flush() { w.flusher.Flush() }

func newLazyPool(dsn string, maxConns int32, timeZone string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	if maxConns > 0 {
		cfg.MaxConns = maxConns
	}
	if _, set := cfg.ConnConfig.RuntimeParams["timezone"]; timeZone != "" && !set {
		cfg.ConnConfig.RuntimeParams["timezone"] = timeZone
	}
	return pgxpool.NewWithConfig(context.Background(), cfg)
}

// Handler 返回该 Runtime 唯一的 Edge handler。
func (r *Runtime) Handler() http.Handler { return r.handler }

// Run 启动 Edge 公网口与可选管理口，直到 ctx 取消。
func (r *Runtime) Run(ctx context.Context) error {
	r.runMu.Lock()
	if r.run {
		r.runMu.Unlock()
		return errors.New("edge runtime already started")
	}
	r.run = true
	r.runMu.Unlock()
	runCtx, stopRuntime := context.WithCancel(ctx)
	var stopAttempts context.CancelFunc
	var watchDone chan struct{}
	var public, admin *http.Server
	defer func() {
		drained := r.handler.deactivate()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		stopRuntime()
		shutdownServers(shutdownCtx, public, admin)
		if watchDone != nil {
			<-watchDone
		}
		select {
		case <-drained:
		case <-shutdownCtx.Done():
			r.log.Warn("canceling edge handlers after shutdown grace period")
			r.handler.cancelRequests()
			<-drained
		}
		if stopAttempts != nil {
			stopAttempts()
			r.attempts.Wait()
		}
		_ = r.rdb.Close()
		if r.pool != nil {
			r.pool.Close()
		}
	}()

	for {
		if err := r.cache.Load(runCtx); err == nil {
			break
		} else {
			r.log.Warn("edge waiting for redis", "err", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(2 * time.Second):
		}
	}
	watchDone = make(chan struct{})
	go func() {
		defer close(watchDone)
		r.cache.Watch(runCtx, 5*time.Minute)
	}()

	// Keep attempts alive through handler drain so completed requests can enqueue their final records.
	attemptCtx, stopFn := context.WithCancel(context.Background())
	stopAttempts = stopFn
	go r.attempts.Run(attemptCtx)
	r.handler.activate()

	public = &http.Server{Addr: r.cfg.Edge.Addr, Handler: r.handler, ReadHeaderTimeout: 10 * time.Second}
	// The compatibility listener is independent of the handler mounted on the host.
	go func() {
		if err := public.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			r.log.Error("edge public listener failed; shared handler remains available", "addr", public.Addr, "err", err)
		}
	}()
	adminErrCh := make(chan error, 1)

	if r.cfg.Edge.AdminAddr != "" {
		adminMux := http.NewServeMux()
		adminMux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
			if !r.cache.Ready() {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			_, _ = w.Write([]byte("ok"))
		})
		adminMux.Handle("GET /debug/vars", expvar.Handler())
		admin = &http.Server{Addr: r.cfg.Edge.AdminAddr, Handler: adminMux, ReadHeaderTimeout: 10 * time.Second}
		go func() { adminErrCh <- admin.ListenAndServe() }()
	}

	r.log.Info("edge listening", "version", buildinfo.Version, "addr", r.cfg.Edge.Addr, "admin", r.cfg.Edge.AdminAddr, "gateway", r.gatewayURL)

	select {
	case err := <-adminErrCh:
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		return err
	case <-ctx.Done():
	}
	return nil
}

func shutdownServers(ctx context.Context, public, admin *http.Server) {
	if public != nil {
		_ = public.Shutdown(ctx)
	}
	if admin != nil {
		_ = admin.Shutdown(ctx)
	}
}

// Run 保留独立 market-edge 入口的既有调用方式。
func Run(ctx context.Context, cfg *config.Config, log *slog.Logger) error {
	runtime, err := NewRuntime(cfg, log)
	if err != nil {
		return err
	}
	return runtime.Run(ctx)
}
