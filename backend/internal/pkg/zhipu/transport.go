package zhipu

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// IsSigningError recognizes the original client's 401 reason fields plus code fields.
func IsSigningError(status int, body []byte) bool {
	if status != http.StatusUnauthorized {
		return false
	}
	var payload struct {
		Code   json.RawMessage `json:"code"`
		Msg    string          `json:"msg"`
		Reason string          `json:"reason"`
		Data   struct {
			Reason string `json:"reason"`
		} `json:"data"`
		Error struct {
			Code    json.RawMessage `json:"code"`
			Reason  string          `json:"reason"`
			Message string          `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return false
	}
	for _, reason := range []string{remoteCode(payload.Code), payload.Msg, payload.Reason, payload.Data.Reason, remoteCode(payload.Error.Code), payload.Error.Reason, payload.Error.Message} {
		if reason == "VERIFY_SIGNATURE_INVALID" || reason == "VERIFY_APIKEY_EXPIRED" {
			return true
		}
	}
	return false
}

// SigningTransport signs at the original business URL, then invokes Base (usually a
// RoutingTransport). After one re-handshake, a second rejection starts a ten-minute
// bypass and allows one unsigned replay. Each replay begins at the original URL.
// A request with a body needs GetBody to be retried. Use a fresh instance (or immutable
// fields) per credential/end-user session; share Signer across these instances.
type SigningTransport struct {
	// Nil reuses the Signer's injected *http.Client transport. Other HTTPClient
	// implementations must supply Base explicitly; never bypass an account proxy.
	Base       http.RoundTripper
	Signer     *Signer
	Credential string
	SessionID  string
}

func (t *SigningTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.Signer == nil || req == nil {
		return nil, errors.New("zhipu: missing transport signer/request")
	}
	base, err := accountTransport(t.Base, t.Signer.client)
	if err != nil {
		closeRequestBody(req)
		return nil, err
	}
	for attempt := 0; attempt < 3; attempt++ {
		out := req.Clone(req.Context())
		if attempt > 0 && req.Body != nil && req.Body != http.NoBody {
			out.Body, err = req.GetBody()
			if err != nil {
				return nil, err
			}
		}
		if err := t.Signer.Sign(req.Context(), out, t.Credential, t.SessionID); err != nil {
			closeRequestBody(out)
			return nil, err
		}
		resp, err := base.RoundTrip(out)
		if err != nil || resp.StatusCode != http.StatusUnauthorized || out.Header.Get("X-Client-Sig") == "" {
			return resp, err
		}
		rejected, err := signatureRejected(resp)
		if err != nil {
			return nil, err
		}
		if !rejected {
			return resp, nil
		}
		t.Signer.invalidateForRequest(t.Credential, out.Header)
		if attempt == 1 {
			t.Signer.Bypass(t.Credential)
		}
		if attempt == 2 || (req.Body != nil && req.Body != http.NoBody && req.GetBody == nil) {
			return resp, nil
		}
		_ = resp.Body.Close()
	}
	return nil, errors.New("zhipu: signing retry limit reached")
}

func signatureRejected(resp *http.Response) (bool, error) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody+1))
	resp.Body = &prefixedBody{Reader: io.MultiReader(bytes.NewReader(body), resp.Body), closer: resp.Body}
	if err != nil {
		_ = resp.Body.Close()
		return false, err
	}
	return len(body) <= maxResponseBody && IsSigningError(resp.StatusCode, body), nil
}

func accountTransport(base http.RoundTripper, client *Client) (http.RoundTripper, error) {
	if base != nil {
		return base, nil
	}
	h, ok := client.http.(*http.Client)
	if !ok {
		return nil, errors.New("zhipu: Base transport required for custom HTTPClient")
	}
	if h.Transport == nil {
		return http.DefaultTransport, nil
	}
	return h.Transport, nil
}

// RoutingTransport resolves cached proxyEndpoint mappings after signing and just
// before sending. Nest it inside SigningTransport.Base; it clones URL/Host and never
// changes the original request used for retries. Base must enforce outbound URL policy.
type RoutingTransport struct {
	Base       http.RoundTripper
	Config     *RemoteConfigCache
	Credential string
}

func (t *RoutingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil || req.URL == nil || t.Config == nil {
		if req != nil {
			closeRequestBody(req)
		}
		return nil, errors.New("zhipu: routing requires request and config cache")
	}
	base, err := accountTransport(t.Base, t.Config.client)
	if err != nil {
		closeRequestBody(req)
		return nil, err
	}
	config, err := t.Config.Get(req.Context(), t.Credential)
	if err != nil {
		if req.Context().Err() != nil {
			closeRequestBody(req)
			return nil, req.Context().Err()
		}
		return base.RoundTrip(req)
	}
	mapped, err := config.MapEndpoint(req.URL)
	if err != nil {
		closeRequestBody(req)
		return nil, err
	}
	out := req.Clone(req.Context())
	out.URL = mapped
	if mapped.Host != req.URL.Host || mapped.Scheme != req.URL.Scheme {
		out.Host = ""
	}
	return base.RoundTrip(out)
}

func closeRequestBody(req *http.Request) {
	if req.Body != nil {
		_ = req.Body.Close()
	}
}

type prefixedBody struct {
	io.Reader
	closer io.Closer
}

func (b *prefixedBody) Close() error { return b.closer.Close() }
