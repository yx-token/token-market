package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/yx-token/token-market/internal/config"
	"github.com/yx-token/token-market/internal/pkg/zhipu"
)

type zhipuSessionContextKey struct{}

// Scope the stable client session/content seed to the authenticated tenant key.
// Missing identity signals get a fresh random session, never a global fallback.
func withZhipuRequestSession(req *http.Request, c *gin.Context, body []byte) *http.Request {
	seed := explicitOpenAIRequestSessionID(c, body)
	if seed == "" {
		seed = deriveOpenAIContentSessionSeed(body)
	}
	if seed == "" || getAPIKeyIDFromContext(c) == 0 {
		var err error
		seed, err = zhipu.GeneratePollToken()
		if err != nil {
			// The send boundary retries entropy generation and surfaces its error.
			return req
		}
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("zhipu:k%d:%s", getAPIKeyIDFromContext(c), seed)))
	return req.WithContext(context.WithValue(req.Context(), zhipuSessionContextKey{}, hex.EncodeToString(digest[:])))
}

type zhipuSigningRuntime struct {
	signer *zhipu.Signer
	config *zhipu.RemoteConfigCache
}

type ZhipuGateway struct {
	upstream HTTPUpstream
	factory  ZhipuClientFactory
	tokens   *ZhipuTokenProvider
	cfg      *config.Config
	mu       sync.Mutex
	runtimes map[[32]byte]*zhipuSigningRuntime
}

func NewZhipuGateway(upstream HTTPUpstream, factory ZhipuClientFactory, tokens *ZhipuTokenProvider, cfg *config.Config) *ZhipuGateway {
	if cfg == nil {
		cfg = &config.Config{}
	}
	return &ZhipuGateway{upstream: upstream, factory: factory, tokens: tokens, cfg: cfg, runtimes: make(map[[32]byte]*zhipuSigningRuntime)}
}

type zhipuRoundTripper func(*http.Request) (*http.Response, error)

func (f zhipuRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

var errZhipuDerivedKeyExpired = errors.New("zhipu derived key rejected")

func zhipuDerivedKeyRejected(status int, body []byte) bool {
	if bytes.Contains(body, []byte("VERIFY_APIKEY_EXPIRED")) || (status == http.StatusUnauthorized && !zhipu.IsSigningError(status, body)) {
		return true
	}
	var payload struct {
		Code  json.RawMessage `json:"code"`
		Error struct {
			Code json.RawMessage `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return false
	}
	return isZhipuCredentialErrorCode(strings.Trim(string(payload.Code), "\"")) || isZhipuCredentialErrorCode(strings.Trim(string(payload.Error.Code), "\""))
}

func (g *ZhipuGateway) runtime(a *Account, req *http.Request, proxy, credential string) (*zhipuSigningRuntime, error) {
	origin := req.URL.Scheme + "://" + req.URL.Host
	hash := sha256.Sum256([]byte(fmt.Sprintf("%d:%s:%s:%s:%s", a.ID, origin, proxy, credential, g.cfg.Gateway.Zhipu.ClientVersion)))
	g.mu.Lock()
	defer g.mu.Unlock()
	if runtime := g.runtimes[hash]; runtime != nil {
		return runtime, nil
	}
	client, err := g.factory(proxy, a.ID, a.Concurrency)
	if err != nil {
		return nil, err
	}
	headers := make(http.Header)
	for key, value := range g.cfg.Gateway.Zhipu.ConfigHeaders {
		headers.Set(key, value)
	}
	cache := zhipu.NewRemoteConfigCache(client, zhipu.RemoteConfigOptions{Headers: headers})
	signer, err := zhipu.NewSigner(client, zhipu.SignerOptions{Origin: origin, Config: cache, Mode: zhipu.SignatureMode(g.cfg.Gateway.Zhipu.SignatureMode)})
	if err != nil {
		return nil, err
	}
	if len(g.runtimes) >= 256 {
		for key := range g.runtimes {
			delete(g.runtimes, key)
			break
		}
	}
	runtime := &zhipuSigningRuntime{signer: signer, config: cache}
	g.runtimes[hash] = runtime
	return runtime, nil
}

func (g *ZhipuGateway) Do(req *http.Request, proxy string, a *Account) (*http.Response, error) {
	if req.Body != nil {
		defer func() { _ = req.Body.Close() }()
	}
	if g.tokens == nil || g.factory == nil || g.upstream == nil {
		return nil, errors.New("zhipu gateway is not configured")
	}
	current, err := g.tokens.Account(req.Context(), a, false)
	if err != nil {
		return nil, err
	}
	session, _ := req.Context().Value(zhipuSessionContextKey{}).(string)
	if session == "" {
		session, err = zhipu.GeneratePollToken()
		if err != nil {
			return nil, err
		}
	}
	for attempt := 0; attempt < 2; attempt++ {
		for _, field := range []string{"oauth_provider", "base_url", "api_base_urls", "api_protocol", "account_mode"} {
			if !reflect.DeepEqual(a.Credentials[field], current.Credentials[field]) {
				// The request was built for a different upstream identity. Let the
				// caller reselect/rebuild instead of sending new credentials there.
				return nil, errOAuthRefreshAccountStateChanged
			}
		}
		// The lock-protected refresh can observe a concurrent proxy change.
		// Resolve the durable account's proxy rather than sending with a stale one.
		if current.ProxyID != nil {
			proxy, err = g.tokens.oauth.proxyURL(req.Context(), current.ProxyID)
			if err != nil {
				return nil, err
			}
		} else if a.ProxyID != nil {
			proxy = ""
		}
		credential := current.GetCredential("api_key")
		if _, _, err := zhipu.SplitCredential(credential); err != nil {
			return nil, err
		}
		runtime, err := g.runtime(current, req, proxy, credential)
		if err != nil {
			return nil, err
		}
		out := req.Clone(req.Context())
		if attempt > 0 && req.Body != nil && req.Body != http.NoBody {
			out.Body, err = req.GetBody()
			if err != nil {
				return nil, err
			}
		}
		out.Header.Set("Authorization", "Bearer "+credential)
		out.Header.Set("X-Session-Id", session)
		if out.Header.Get("X-Api-Key") != "" || strings.Contains(out.URL.Path, "/anthropic/") {
			out.Header.Set("X-Api-Key", credential)
		}
		base := zhipuRoundTripper(func(mapped *http.Request) (*http.Response, error) {
			ctx := WithHTTPUpstreamRedirectsDisabled(mapped.Context())
			ctx = WithHTTPUpstreamPublicHostsOnly(ctx)
			resp, err := g.upstream.Do(mapped.WithContext(ctx), proxy, current.ID, current.Concurrency)
			if err != nil || (resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusBadRequest) {
				return resp, err
			}
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
			if readErr != nil {
				_ = resp.Body.Close()
				return nil, readErr
			}
			if zhipuDerivedKeyRejected(resp.StatusCode, body) {
				_ = resp.Body.Close()
				return nil, errZhipuDerivedKeyExpired
			}
			resp.Body = &zhipuReplayBody{Reader: io.MultiReader(bytes.NewReader(body), resp.Body), closer: resp.Body}
			return resp, nil
		})
		transport := &zhipu.SigningTransport{Signer: runtime.signer, Credential: credential, SessionID: session, Base: &zhipu.RoutingTransport{Config: runtime.config, Credential: credential, Base: base}}
		resp, err := transport.RoundTrip(out)
		var apiErr *zhipu.APIError
		if errors.As(err, &apiErr) && (apiErr.Status == http.StatusUnauthorized || isZhipuCredentialErrorCode(apiErr.Code)) {
			err = errZhipuDerivedKeyExpired
		}
		if !errors.Is(err, errZhipuDerivedKeyExpired) {
			return resp, err
		}
		runtime.signer.Invalidate(credential)
		if attempt == 1 {
			if markErr := g.tokens.RequireReauthorization(req.Context(), current); markErr != nil {
				return nil, markErr
			}
			return nil, ErrZhipuReauthorizationRequired
		}
		if req.Body != nil && req.Body != http.NoBody && req.GetBody == nil {
			return nil, errZhipuDerivedKeyExpired
		}
		current, err = g.tokens.Account(req.Context(), current, true)
		if err != nil {
			return nil, err
		}
	}
	return nil, errZhipuDerivedKeyExpired
}

type zhipuReplayBody struct {
	io.Reader
	closer io.Closer
}

func (b *zhipuReplayBody) Close() error { return b.closer.Close() }
