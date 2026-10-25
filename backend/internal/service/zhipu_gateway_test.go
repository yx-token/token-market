//go:build unit

package service

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/yx-token/token-market/internal/pkg/tlsfingerprint"
	"github.com/yx-token/token-market/internal/pkg/zhipu"
)

type zhipuTestUpstream struct{ client *http.Client }

func (u zhipuTestUpstream) Do(r *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return u.client.Do(r)
}
func (u zhipuTestUpstream) DoWithTLS(r *http.Request, p string, id int64, n int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(r, p, id, n)
}

func zhipuServerKDF(secret, info string) []byte {
	extract := hmac.New(sha256.New, []byte("WD_CLIENT_SIGN_KDF_SALT"))
	_, _ = extract.Write([]byte(secret))
	expand := hmac.New(sha256.New, extract.Sum(nil))
	_, _ = expand.Write(append([]byte(info), 1))
	return expand.Sum(nil)
}

type zhipuGatewayFixture struct {
	oauth           *zhipuOAuthFixture
	repo            *zhipuAccountRepoTest
	gateway         *ZhipuGateway
	service         *OpenAIGatewayService
	account         *Account
	handshakes      atomic.Int32
	rejectKeys      atomic.Int32
	rejectHandshake atomic.Bool
	mu              sync.Mutex
	requests        []http.Header
	paths           []string
}

func newZhipuGatewayFixture(t *testing.T) *zhipuGatewayFixture {
	t.Helper()
	f := newZhipuOAuthFixture(t)
	g := &zhipuGatewayFixture{oauth: f, account: zhipuTestAccount(zhipu.ProviderBigModel)}
	g.account.Credentials["api_key"] = "derived.secret"
	g.account.Credentials["base_url"] = f.server.URL + "/api/coding/paas/v4"
	public, private, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	f.extra = func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/paas/c1f3a7e2/v2/client":
			g.handshakes.Add(1)
			if g.rejectHandshake.Swap(false) {
				w.WriteHeader(401)
				zhipuJSON(t, w, 401, map[string]any{})
				return
			}
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
				return
			}
			mac := hmac.New(sha256.New, zhipuServerKDF("secret", "getSignKey_hmac"))
			_, _ = mac.Write([]byte("get_sign_key\nderived\n" + body["ts"] + "\n" + body["nonce"]))
			sig, err := base64.StdEncoding.DecodeString(body["sig"])
			if err != nil || !hmac.Equal(sig, mac.Sum(nil)) || r.Header.Get("Authorization") != "derived.secret" {
				t.Error("invalid handshake HMAC/credential")
			}
			der, err := x509.MarshalPKCS8PrivateKey(private)
			if err != nil {
				t.Error(err)
				return
			}
			block, err := aes.NewCipher(zhipuServerKDF("secret", "ed25519_priv"))
			if err != nil {
				t.Error(err)
				return
			}
			aead, err := cipher.NewGCM(block)
			if err != nil {
				t.Error(err)
				return
			}
			iv := make([]byte, aead.NonceSize())
			if _, err := rand.Read(iv); err != nil {
				t.Error(err)
				return
			}
			packed := append(iv, aead.Seal(nil, iv, []byte(base64.StdEncoding.EncodeToString(der)), []byte("derived"))...)
			zhipuJSON(t, w, 200, map[string]any{"privateCipher": base64.StdEncoding.EncodeToString(packed)})
		case "/api/v1/agent/configs":
			if r.Header.Get("X-Api-Key") != "derived.secret" {
				t.Error("config must use full derived key")
			}
			zhipuJSON(t, w, 0, map[string]any{"codingPlanSignature": map[string]any{"enable": true}, "proxyEndpoint": map[string]any{"mapping": []any{map[string]any{"from": f.server.URL + "/api/coding/paas/v4/chat/completions", "to": f.server.URL + "/mapped/chat/completions"}}}})
		case "/mapped/chat/completions", "/api/anthropic/v1/messages":
			h := r.Header
			session := h.Get("X-Session-Id")
			ts := h.Get("X-Client-Ts")
			if h.Get("Authorization") != "Bearer derived.secret" || session == "" || h.Get("User-Agent") != "ZCode/3.11.2" {
				t.Error("wrong inference identity")
			}
			if strings.Contains(r.URL.Path, "anthropic") && h.Get("X-Api-Key") != "derived.secret" {
				t.Error("Anthropic must have both auth headers")
			}
			sig, err := base64.StdEncoding.DecodeString(h.Get("X-Client-Sig"))
			message := strings.Join([]string{"derived", ts, "3.11.2", session, h.Get("X-Client-Nonce")}, "\n")
			if err != nil || !ed25519.Verify(public, []byte(message), sig) {
				t.Error("invalid inference signature")
			}
			seed := sha256.Sum256([]byte("derived\nzcode\n" + session + "\n" + ts))
			pow := sha256.Sum256([]byte(hex.EncodeToString(seed[:])[:32] + "\n" + h.Get("X-Client-Pow")))
			if pow[0] != 0 {
				t.Error("invalid PoW")
			}
			body, err := io.ReadAll(r.Body)
			if err != nil || len(body) == 0 {
				t.Error("missing/replay body")
			}
			g.mu.Lock()
			g.requests = append(g.requests, h.Clone())
			g.paths = append(g.paths, r.URL.Path)
			g.mu.Unlock()
			if g.rejectKeys.Load() > 0 {
				g.rejectKeys.Add(-1)
				w.WriteHeader(401)
				_, _ = w.Write([]byte(`{"reason":"VERIFY_APIKEY_EXPIRED"}`))
				return
			}
			var payload struct {
				Stream bool `json:"stream"`
			}
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Error(err)
			}
			if payload.Stream {
				w.Header().Set("Content-Type", "text/event-stream")
				if strings.Contains(r.URL.Path, "anthropic") {
					resp := nativeAnthropicStreamResponse()
					defer func() { _ = resp.Body.Close() }()
					if _, err := io.Copy(w, resp.Body); err != nil {
						t.Error(err)
					}
				} else {
					_, _ = w.Write([]byte("data: {\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"model\":\"glm-5\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"},\"finish_reason\":null}]}\n\ndata: {\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"model\":\"glm-5\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\ndata: [DONE]\n\n"))
				}
				return
			}
			if strings.Contains(r.URL.Path, "anthropic") {
				_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"glm-5","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":"chat_1","object":"chat.completion","model":"glm-5","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
		default:
			t.Errorf("unexpected GLM inference URL %s", r.URL)
			w.WriteHeader(404)
		}
	}
	p, repo := zhipuTestProvider(f, g.account)
	g.repo = repo
	cfg := rawChatCompletionsTestConfig()
	cfg.Gateway.Zhipu.SignatureMode = "always"
	cfg.Gateway.Zhipu.ClientVersion = zhipu.ClientVersion
	u := zhipuTestUpstream{client: f.server.Client()}
	g.gateway = NewZhipuGateway(u, f.service.factory, p, cfg)
	g.service = &OpenAIGatewayService{cfg: cfg, httpUpstream: u, zhipuTokenProvider: p, zhipuGateway: g.gateway}
	return g
}

func zhipuTenantContext(path string, body []byte, id int64) *gin.Context {
	c := adaptiveProtocolTestContext(path, body)
	c.Set("api_key", &APIKey{ID: id})
	c.Request.Header.Set("Session_id", "same-user-session")
	return c
}

func TestZhipuOAuthGatewayChatResponsesAnthropicAndAdaptive(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, protocol := range []string{APIProtocolChatCompletions, APIProtocolAdaptive, APIProtocolAnthropic} {
		t.Run(protocol, func(t *testing.T) {
			g := newZhipuGatewayFixture(t)
			g.account.Credentials["api_protocol"] = protocol
			g.account.Credentials["api_base_urls"] = map[string]any{APIProtocolChatCompletions: g.oauth.server.URL + "/api/coding/paas/v4", APIProtocolAnthropic: g.oauth.server.URL + "/api/anthropic"}
			if protocol == APIProtocolAnthropic {
				g.account.Credentials["base_url"] = g.oauth.server.URL + "/api/anthropic"
			}
			g.repo.a = snapshotOAuthRefreshAccount(g.account)
			for _, tc := range []struct{ path, body string }{
				{"/v1/chat/completions", `{"model":"glm-5","messages":[{"role":"user","content":"hello"}],"stream":false}`},
				{"/v1/responses", `{"model":"glm-5","input":"hello","stream":false}`},
				{"/v1/messages", `{"model":"glm-5","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":false}`},
			} {
				body := []byte(tc.body)
				c := zhipuTenantContext(tc.path, body, 11)
				var err error
				switch tc.path {
				case "/v1/chat/completions":
					_, err = g.service.ForwardAsChatCompletions(context.Background(), c, g.account, body, "", "")
				case "/v1/responses":
					_, err = g.service.Forward(context.Background(), c, g.account, body)
				default:
					_, err = g.service.ForwardAsAnthropic(context.Background(), c, g.account, body, "", "")
				}
				require.NoError(t, err, tc.path)
				require.Equal(t, 200, c.Writer.Status())
			}
			require.Len(t, g.requests, 3)
			require.EqualValues(t, 1, g.handshakes.Load())
			for _, h := range g.requests {
				require.Equal(t, g.requests[0].Get("X-Session-Id"), h.Get("X-Session-Id"))
				require.NotEmpty(t, h.Get("X-Client-Sig"))
			}
		})
	}
}

func TestZhipuOAuthGatewaySessionIsolation(t *testing.T) {
	g := newZhipuGatewayFixture(t)
	body := []byte(`{"model":"glm-5","messages":[]}`)
	for _, id := range []int64{11, 11, 12} {
		req, err := http.NewRequest(http.MethodPost, g.oauth.server.URL+"/api/coding/paas/v4/chat/completions", bytes.NewReader(body))
		require.NoError(t, err)
		req = withZhipuRequestSession(req, zhipuTenantContext("/v1/chat/completions", body, id), body)
		resp, err := g.gateway.Do(req, "", g.account)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
	}
	require.Len(t, g.requests, 3)
	require.Equal(t, g.requests[0].Get("X-Session-Id"), g.requests[1].Get("X-Session-Id"))
	require.NotEqual(t, g.requests[0].Get("X-Session-Id"), g.requests[2].Get("X-Session-Id"))
}

func TestZhipuOAuthGatewayRederivesRejectedKey(t *testing.T) {
	for _, handshake := range []bool{false, true} {
		t.Run(map[bool]string{false: "inference", true: "handshake"}[handshake], func(t *testing.T) {
			g := newZhipuGatewayFixture(t)
			if handshake {
				g.rejectHandshake.Store(true)
			} else {
				g.rejectKeys.Store(1)
			}
			req, err := http.NewRequest(http.MethodPost, g.oauth.server.URL+"/api/coding/paas/v4/chat/completions", strings.NewReader(`{"messages":[]}`))
			require.NoError(t, err)
			resp, err := g.gateway.Do(req, "", g.account)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			require.EqualValues(t, 1, g.oauth.copies.Load())
			require.Equal(t, 1, g.repo.writes)
			require.Equal(t, StatusActive, g.repo.a.Status)
			require.EqualValues(t, 2, g.handshakes.Load())
		})
	}
}

func TestZhipuOAuthGatewayRejectedAfterRederivationNeedsAuthorization(t *testing.T) {
	g := newZhipuGatewayFixture(t)
	g.rejectKeys.Store(2)
	req, err := http.NewRequest(http.MethodPost, g.oauth.server.URL+"/api/coding/paas/v4/chat/completions", strings.NewReader(`{"messages":[]}`))
	require.NoError(t, err)
	_, err = g.gateway.Do(req, "", g.account)
	require.ErrorIs(t, err, ErrZhipuReauthorizationRequired)
	require.Equal(t, StatusError, g.repo.a.Status)
	require.False(t, g.repo.a.Schedulable)
}

func TestZhipuOAuthAccountProbeUsesSigningGateway(t *testing.T) {
	g := newZhipuGatewayFixture(t)
	g.account.Credentials["api_protocol"] = APIProtocolAdaptive
	g.account.Credentials["api_base_urls"] = map[string]any{APIProtocolChatCompletions: g.oauth.server.URL + "/api/coding/paas/v4", APIProtocolAnthropic: g.oauth.server.URL + "/api/anthropic"}
	g.repo.a = snapshotOAuthRefreshAccount(g.account)
	s := &AccountTestService{accountRepo: g.repo, httpUpstream: g.gateway.upstream, cfg: g.service.cfg, openaiGatewayService: g.service}
	c, recorder := newTestContext()
	require.NoError(t, s.TestAccountConnection(c, g.account.ID, "glm-5", "", AccountTestModeDefault))
	require.Len(t, g.requests, 2)
	require.Contains(t, recorder.Body.String(), `"type":"test_complete"`)
}
