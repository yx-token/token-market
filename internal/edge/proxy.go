package edge

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"expvar"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/DueGin/token-market/internal/edgecfg"
	"github.com/DueGin/token-market/internal/platform/clientip"
	"github.com/DueGin/token-market/internal/platform/crypto"
	"github.com/DueGin/token-market/internal/platform/ratelimit"
)

var (
	metricRequests  = expvar.NewInt("edge_requests_total")
	metricAttempts  = expvar.NewMap("edge_attempts_total")
	metricFailovers = expvar.NewInt("edge_failovers_total")
	metricBreakers  = expvar.NewInt("edge_breaker_open_total")
	// metricAuthThrottled 统计因无效 Key 过多被拒绝的请求
	metricAuthThrottled = expvar.NewInt("edge_auth_throttled_total")
)

type Config struct {
	GatewayURL       *url.URL
	ConnectTimeout   time.Duration
	FirstByteTimeout time.Duration
	MaxBodyBytes     int64
	BreakerFailures  int
	BreakerCooldown  time.Duration
	// TrustedProxies 是前置负载均衡的地址（IP 或 CIDR）；只有来自它们的请求才采信 X-Forwarded-For
	TrustedProxies []string
	// InvalidKeyLimit 是单个 IP 每分钟允许的无效 Key 次数，超过后该分钟内直接拒绝；0 表示不限制
	InvalidKeyLimit int
}

type Server struct {
	cfg      Config
	cache    *Cache
	keyring  *crypto.Keyring
	attempts *AttemptLogger
	breaker  *breaker
	ips      clientip.Resolver
	authFail *ratelimit.Failures
	client   *http.Client
	log      *slog.Logger
	now      func() time.Time
}

func NewServer(cfg Config, cache *Cache, kr *crypto.Keyring, attempts *AttemptLogger, log *slog.Logger) *Server {
	transport := &http.Transport{
		Proxy:               nil,
		DialContext:         (&net.Dialer{Timeout: cfg.ConnectTimeout, KeepAlive: 30 * time.Second}).DialContext,
		MaxIdleConns:        512,
		MaxIdleConnsPerHost: 256,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: cfg.ConnectTimeout,
		ForceAttemptHTTP2:   true,
		// 不让 Transport 自动解压，原样透传 sub2api 的压缩响应
		DisableCompression: true,
	}
	return &Server{
		cfg: cfg, cache: cache, keyring: kr, attempts: attempts,
		breaker:  newBreaker(cfg.BreakerFailures, cfg.BreakerCooldown),
		ips:      clientip.New(cfg.TrustedProxies),
		authFail: ratelimit.NewFailures(cfg.InvalidKeyLimit, time.Minute),
		client: &http.Client{
			Transport: transport,
			// 不跟随重定向，原样返回
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		log: log, now: time.Now,
	}
}

// allowedPath 只放行对话相关的网关路径。
func allowedPath(method, p string) bool {
	switch {
	case p == "/v1/messages" || p == "/v1/messages/count_tokens" || p == "/v1/chat/completions":
		return method == http.MethodPost
	case p == "/v1/responses":
		return method == http.MethodPost || method == http.MethodGet
	case strings.HasPrefix(p, "/v1/responses/"):
		return method == http.MethodPost
	case p == "/v1/models" || strings.HasPrefix(p, "/v1/models/"):
		return method == http.MethodGet
	case p == "/v1/realtime":
		return method == http.MethodGet
	case strings.HasPrefix(p, "/v1beta/models"),
		strings.HasPrefix(p, "/backend-api/codex/"),
		strings.HasPrefix(p, "/antigravity/v1/"),
		strings.HasPrefix(p, "/antigravity/v1beta/"):
		return true
	}
	return false
}

// billablePath 判断接口是否会产生费用（用于对账：模型列表、count_tokens 成功但不计费）。
func billablePath(method, p string) bool {
	switch {
	case method == http.MethodGet && (p == "/v1/models" || strings.HasPrefix(p, "/v1/models/") || strings.HasPrefix(p, "/v1beta/models")):
		return false
	case p == "/v1/messages/count_tokens" || strings.HasSuffix(p, ":countTokens"):
		return false
	}
	return true
}

// keyLocation 记录买家 Key 出现的位置，转发时把内部 key 放回同一位置。
type keyLocation int

const (
	locNone keyLocation = iota
	locBearer
	locXAPIKey
	locGoogHeader
	locQuery
)

func extractKey(r *http.Request) (string, keyLocation) {
	if v, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok && strings.HasPrefix(v, "tm-") {
		return strings.TrimSpace(v), locBearer
	}
	if v := r.Header.Get("x-api-key"); strings.HasPrefix(v, "tm-") {
		return v, locXAPIKey
	}
	if v := r.Header.Get("x-goog-api-key"); strings.HasPrefix(v, "tm-") {
		return v, locGoogHeader
	}
	if v := r.URL.Query().Get("key"); strings.HasPrefix(v, "tm-") {
		return v, locQuery
	}
	return "", locNone
}

func writeError(w http.ResponseWriter, status int, typ, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"type": typ, "message": msg}})
}

func newRequestID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "tmr_" + hex.EncodeToString(b)
}

// hopByHop 是不能跨代理转发的头。
var hopByHop = map[string]bool{
	"Connection": true, "Proxy-Connection": true, "Keep-Alive": true, "Proxy-Authenticate": true,
	"Proxy-Authorization": true, "Te": true, "Trailer": true, "Transfer-Encoding": true, "Upgrade": true,
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	metricRequests.Add(1)
	if !s.cache.Ready() {
		writeError(w, http.StatusServiceUnavailable, "api_error", "service starting")
		return
	}
	if !allowedPath(r.Method, r.URL.Path) {
		writeError(w, http.StatusNotFound, "not_found_error", "unsupported endpoint")
		return
	}
	ip := s.ips.Resolve(r.RemoteAddr, r.Header.Values("X-Forwarded-For"))
	if s.authFail.Blocked(ip) {
		metricAuthThrottled.Add(1)
		writeError(w, http.StatusTooManyRequests, "rate_limit_error", "too many invalid api keys, retry later")
		return
	}
	key, loc := extractKey(r)
	if key == "" {
		writeError(w, http.StatusUnauthorized, "authentication_error", "missing api key")
		return
	}
	cfg := s.cache.Key(edgecfg.HashKey(key))
	if cfg == nil {
		s.authFail.Fail(ip)
		writeError(w, http.StatusUnauthorized, "authentication_error", "invalid api key")
		return
	}
	now := s.now()
	switch {
	case cfg.Status != "active":
		writeError(w, http.StatusForbidden, "permission_error", "api key disabled: "+cfg.Reason)
		return
	case cfg.ExpiresAt != nil && now.After(*cfg.ExpiresAt):
		writeError(w, http.StatusUnauthorized, "authentication_error", "api key expired")
		return
	case cfg.BlockedUntil != nil && now.Before(*cfg.BlockedUntil):
		writeError(w, http.StatusTooManyRequests, "rate_limit_error", "spending limit reached: "+cfg.Reason)
		return
	case len(cfg.IPWhitelist) > 0 && !ipAllowed(ip, cfg.IPWhitelist):
		writeError(w, http.StatusForbidden, "permission_error", "ip not allowed")
		return
	}

	reqID := newRequestID()
	w.Header().Set("X-TM-Request-ID", reqID)
	if isWebSocket(r) {
		s.serveWebSocket(w, r, cfg, key, loc, reqID)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, s.cfg.MaxBodyBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "cannot read request body")
		return
	}
	if int64(len(body)) > s.cfg.MaxBodyBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "invalid_request_error", "request body too large")
		return
	}
	deadline := now.Add(s.cfg.FirstByteTimeout)
	billable := billablePath(r.Method, r.URL.Path)
	var last *failedResponse
	tried := 0
	for i, hop := range cfg.Hops {
		a := Attempt{EdgeRequestID: reqID, BuyerKeyID: cfg.BuyerKeyID, Hop: i + 1, ListingID: hop.ListingID, PoolID: hop.PoolID,
			Billable: billable, StartedAt: s.now()}
		if (hop.ListingID > 0 && s.cache.Skipped(hop.ListingID)) || !s.breaker.Allow(hop.Label) {
			a.EndReason, a.EndedAt = "skipped", a.StartedAt
			s.record(a)
			continue
		}
		if !s.now().Before(deadline) {
			break
		}
		internalKey, err := s.keyring.OpenString(hop.InternalKeyEnc, hop.AAD())
		if err != nil {
			s.log.Error("decrypt internal key", "internal_key_id", hop.InternalKeyID, "err", err)
			continue
		}
		if tried > 0 {
			metricFailovers.Add(1)
		}
		tried++
		done, fr := s.forward(w, r, body, internalKey, loc, hop, i+1, deadline, &a)
		if done {
			return
		}
		last = fr
	}
	if last != nil {
		last.write(w)
		return
	}
	writeError(w, http.StatusServiceUnavailable, "overloaded_error", "no seller available for this key right now")
}

// failedResponse 是可重试失败的响应，所有跳都失败时返回最后一个。
type failedResponse struct {
	status int
	header http.Header
	body   []byte
}

func (f *failedResponse) write(w http.ResponseWriter) {
	for k, vs := range f.header {
		if hopByHop[k] || k == "Content-Length" {
			continue
		}
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(f.status)
	_, _ = w.Write(f.body)
}

func retryableStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// forward 转发到一跳。返回 done=true 表示响应已写给买家（成功或不可重试的失败）。
func (s *Server) forward(w http.ResponseWriter, r *http.Request, body []byte, internalKey string, loc keyLocation,
	hop edgecfg.Hop, hopNo int, deadline time.Time, a *Attempt) (bool, *failedResponse) {
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	var timedOut atomic.Bool
	timer := time.AfterFunc(time.Until(deadline), func() { timedOut.Store(true); cancel() })

	req, err := http.NewRequestWithContext(ctx, r.Method, s.targetURL(r, internalKey, loc), bytes.NewReader(body))
	if err != nil {
		timer.Stop()
		a.EndReason, a.EndedAt = "upstream_error", s.now()
		s.record(*a)
		return false, nil
	}
	copyRequestHeaders(req.Header, r.Header)
	setKey(req, internalKey, loc)
	req.ContentLength = int64(len(body))

	resp, err := s.client.Do(req)
	stopped := timer.Stop()
	if err != nil {
		reason := "upstream_error"
		if timedOut.Load() || (!stopped && errors.Is(err, context.Canceled)) {
			reason = "timeout"
		}
		if r.Context().Err() != nil {
			reason = "client_cancel"
		}
		a.EndReason, a.EndedAt = reason, s.now()
		s.record(*a)
		if reason == "client_cancel" {
			return true, nil
		}
		if s.breaker.Failure(hop.Label) {
			metricBreakers.Add(1)
		}
		return false, nil
	}
	defer func() { _ = resp.Body.Close() }()
	a.HTTPStatus = resp.StatusCode
	a.Sub2APIRequestID = resp.Header.Get("X-Client-Request-ID")
	a.TTFBMs = int(s.now().Sub(a.StartedAt).Milliseconds())

	if retryableStatus(resp.StatusCode) {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		a.EndReason, a.EndedAt = "retry", s.now()
		s.record(*a)
		if s.breaker.Failure(hop.Label) {
			metricBreakers.Add(1)
		}
		return false, &failedResponse{status: resp.StatusCode, header: resp.Header.Clone(), body: b}
	}
	s.breaker.Success(hop.Label)

	h := w.Header()
	for k, vs := range resp.Header {
		if hopByHop[k] {
			continue
		}
		for _, v := range vs {
			h.Add(k, v)
		}
	}
	h.Set("X-TM-Listing", hop.Label)
	h.Set("X-TM-Hop", strconv.Itoa(hopNo))
	w.WriteHeader(resp.StatusCode)
	a.Committed = true
	a.EndReason = streamBody(w, resp.Body, r.Context())
	a.EndedAt = s.now()
	s.record(*a)
	return true, nil
}

// streamBody 边读边写并及时 flush（SSE 流式响应），返回结束原因。
func streamBody(w http.ResponseWriter, body io.Reader, clientCtx context.Context) string {
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 32<<10)
	for {
		n, err := body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return "client_cancel"
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err == io.EOF {
			return "completed"
		}
		if err != nil {
			if clientCtx.Err() != nil {
				return "client_cancel"
			}
			return "upstream_error"
		}
	}
}

func (s *Server) record(a Attempt) {
	metricAttempts.Add(a.EndReason, 1)
	if s.attempts != nil {
		s.attempts.Record(a)
	}
}

func (s *Server) targetURL(r *http.Request, internalKey string, loc keyLocation) string {
	u := *s.cfg.GatewayURL
	u.Path = strings.TrimRight(u.Path, "/") + r.URL.Path
	q := r.URL.Query()
	if loc == locQuery {
		q.Set("key", internalKey)
	} else if strings.HasPrefix(q.Get("key"), "tm-") {
		q.Del("key")
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func copyRequestHeaders(dst, src http.Header) {
	for k, vs := range src {
		if hopByHop[k] || k == "Authorization" || k == "X-Api-Key" || k == "X-Goog-Api-Key" || k == "Content-Length" {
			continue
		}
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
	dst.Del("X-Forwarded-For")
}

func setKey(req *http.Request, key string, loc keyLocation) {
	switch loc {
	case locBearer:
		req.Header.Set("Authorization", "Bearer "+key)
	case locXAPIKey:
		req.Header.Set("x-api-key", key)
	case locGoogHeader:
		req.Header.Set("x-goog-api-key", key)
	}
}

func ipAllowed(ip string, list []string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, entry := range list {
		if strings.Contains(entry, "/") {
			if _, n, err := net.ParseCIDR(entry); err == nil && n.Contains(parsed) {
				return true
			}
		} else if other := net.ParseIP(entry); other != nil && other.Equal(parsed) {
			return true
		}
	}
	return false
}

func isWebSocket(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") && strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}

// serveWebSocket 只在握手阶段做故障转移，握手成功后双向透传。
func (s *Server) serveWebSocket(w http.ResponseWriter, r *http.Request, cfg *edgecfg.KeyConfig, key string, loc keyLocation, reqID string) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		writeError(w, http.StatusInternalServerError, "api_error", "websocket not supported")
		return
	}
	for i, hop := range cfg.Hops {
		a := Attempt{EdgeRequestID: reqID, BuyerKeyID: cfg.BuyerKeyID, Hop: i + 1, ListingID: hop.ListingID, PoolID: hop.PoolID,
			Billable: true, StartedAt: s.now()}
		if (hop.ListingID > 0 && s.cache.Skipped(hop.ListingID)) || !s.breaker.Allow(hop.Label) {
			a.EndReason, a.EndedAt = "skipped", a.StartedAt
			s.record(a)
			continue
		}
		internalKey, err := s.keyring.OpenString(hop.InternalKeyEnc, hop.AAD())
		if err != nil {
			continue
		}
		upstream, resp, err := s.dialWebSocket(r, internalKey, loc)
		if err != nil {
			a.EndReason, a.EndedAt = "upstream_error", s.now()
			s.record(a)
			s.breaker.Failure(hop.Label)
			continue
		}
		a.HTTPStatus = resp.StatusCode
		a.Sub2APIRequestID = resp.Header.Get("X-Client-Request-ID")
		if resp.StatusCode != http.StatusSwitchingProtocols && retryableStatus(resp.StatusCode) {
			_ = upstream.Close()
			a.EndReason, a.EndedAt = "retry", s.now()
			s.record(a)
			s.breaker.Failure(hop.Label)
			continue
		}
		s.breaker.Success(hop.Label)
		client, buf, err := hj.Hijack()
		if err != nil {
			_ = upstream.Close()
			return
		}
		resp.Header.Set("X-TM-Listing", hop.Label)
		resp.Header.Set("X-TM-Request-ID", reqID)
		_ = resp.Write(client)
		a.Committed = true
		if resp.StatusCode == http.StatusSwitchingProtocols {
			if buf.Reader.Buffered() > 0 {
				pending, _ := buf.Peek(buf.Reader.Buffered())
				_, _ = upstream.Write(pending)
			}
			pipe(client, upstream)
			a.EndReason = "completed"
		} else {
			_ = client.Close()
			_ = upstream.Close()
			a.EndReason = "completed"
		}
		a.EndedAt = s.now()
		s.record(a)
		return
	}
	writeError(w, http.StatusServiceUnavailable, "overloaded_error", "no seller available for this key right now")
}

func (s *Server) dialWebSocket(r *http.Request, internalKey string, loc keyLocation) (net.Conn, *http.Response, error) {
	target, err := url.Parse(s.targetURL(r, internalKey, loc))
	if err != nil {
		return nil, nil, err
	}
	host := target.Host
	if target.Port() == "" {
		if target.Scheme == "https" {
			host += ":443"
		} else {
			host += ":80"
		}
	}
	d := &net.Dialer{Timeout: s.cfg.ConnectTimeout}
	var conn net.Conn
	if target.Scheme == "https" {
		conn, err = tls.DialWithDialer(d, "tcp", host, &tls.Config{ServerName: target.Hostname(), MinVersion: tls.VersionTLS12})
	} else {
		conn, err = d.Dial("tcp", host)
	}
	if err != nil {
		return nil, nil, err
	}
	req, err := http.NewRequest(http.MethodGet, target.String(), nil)
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	for k, vs := range r.Header {
		if k == "Authorization" || k == "X-Api-Key" || k == "X-Goog-Api-Key" {
			continue
		}
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	setKey(req, internalKey, loc)
	_ = conn.SetDeadline(time.Now().Add(s.cfg.FirstByteTimeout))
	if err := req.Write(conn); err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("read handshake: %w", err)
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, resp, nil
}

func pipe(a, b net.Conn) {
	done := make(chan struct{}, 2)
	cp := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		done <- struct{}{}
	}
	go cp(a, b)
	go cp(b, a)
	<-done
	_ = a.Close()
	_ = b.Close()
	<-done
}
