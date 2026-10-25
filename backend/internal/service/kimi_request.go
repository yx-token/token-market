package service

import (
	"context"
	"net/http"
	"strings"

	"github.com/yx-token/token-market/internal/pkg/kimi"
)

// protocolCredential is the single entry for protocol-family credentials.
// Account's API-key getter deliberately cannot return Kimi OAuth secrets.
func protocolCredential(ctx context.Context, account *Account, provider *KimiTokenProvider) (string, error) {
	if account.isKimiOAuth() {
		if provider == nil {
			return "", errKimiOAuthRefreshNotConfigured
		}
		return provider.GetAccessToken(ctx, account)
	}
	return strings.TrimSpace(account.GetOpenAIProtocolAPIKey()), nil
}

// Do authenticates every Kimi HTTP exit, including probes and protocol
// fallbacks. Only an unconsumed HTTP 401 response can trigger one replay.
func (p *KimiTokenProvider) Do(req *http.Request, account *Account, send func(*http.Request) (*http.Response, error)) (*http.Response, error) {
	if p == nil {
		return nil, errKimiOAuthRefreshNotConfigured
	}
	token, err := p.GetAccessToken(req.Context(), account)
	if err != nil {
		return nil, err
	}
	req = req.Clone(req.Context())
	req.Header.Del("x-api-key")
	kimi.ApplyDeviceHeaders(req.Header, account.GetCredential("device_id"))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := send(req)
	if err != nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
		return resp, err
	}
	if req.Body != nil && req.GetBody == nil {
		return resp, nil
	}
	// Anchor force-refresh to the token actually rejected, not the scheduler's
	// possibly older snapshot. The refresh API rereads the authoritative row.
	attempted := snapshotOAuthRefreshAccount(account)
	attempted.Credentials["access_token"] = token
	latest, refreshErr := p.refreshAccount(req.Context(), attempted, false)
	if refreshErr != nil {
		_ = resp.Body.Close()
		return nil, refreshErr
	}
	if latest == nil {
		_ = resp.Body.Close()
		return nil, errOAuthRefreshAccountStateChanged
	}
	retry := req.Clone(req.Context())
	if req.GetBody != nil {
		retry.Body, err = req.GetBody()
		if err != nil {
			_ = resp.Body.Close()
			return nil, err
		}
	}
	_ = resp.Body.Close()
	kimi.ApplyDeviceHeaders(retry.Header, latest.GetCredential("device_id"))
	retry.Header.Set("Authorization", "Bearer "+latest.GetCredential("access_token"))
	resp, err = send(retry)
	if err == nil && resp != nil && resp.StatusCode == http.StatusUnauthorized {
		if stateErr := p.refreshAPI.recordKimiFailure(req.Context(), latest, errKimiOAuthAccessTokenExpired); stateErr != nil {
			_ = resp.Body.Close()
			return nil, stateErr
		}
	}
	return resp, err
}

func (s *AccountTestService) protocolCredential(ctx context.Context, a *Account) (string, error) {
	if !a.isKimiOAuth() {
		return a.GetOpenAIProtocolAPIKey(), nil
	}
	return protocolCredential(ctx, a, s.kimiTokenProvider)
}

// Dial applies the same lifecycle to a WebSocket handshake. No client frame has
// been sent at this point, so one 401 retry cannot duplicate inference work.
func (p *KimiTokenProvider) Dial(ctx context.Context, a *Account, wsURL string, headers http.Header, proxy string, dialer openAIWSClientDialer) (openAIWSClientConn, int, http.Header, error) {
	if p == nil {
		return nil, 0, nil, errKimiOAuthRefreshNotConfigured
	}
	token, err := p.GetAccessToken(ctx, a)
	if err != nil {
		return nil, 0, nil, err
	}
	headers = cloneHeader(headers)
	kimi.ApplyDeviceHeaders(headers, a.GetCredential("device_id"))
	headers.Set("Authorization", "Bearer "+token)
	conn, status, responseHeaders, err := dialer.Dial(ctx, wsURL, headers, proxy)
	if status != http.StatusUnauthorized {
		return conn, status, responseHeaders, err
	}
	attempted := snapshotOAuthRefreshAccount(a)
	attempted.Credentials["access_token"] = token
	latest, refreshErr := p.refreshAccount(ctx, attempted, false)
	if refreshErr != nil {
		return nil, status, responseHeaders, refreshErr
	}
	headers = cloneHeader(headers)
	kimi.ApplyDeviceHeaders(headers, latest.GetCredential("device_id"))
	headers.Set("Authorization", "Bearer "+latest.GetCredential("access_token"))
	conn, status, responseHeaders, err = dialer.Dial(ctx, wsURL, headers, proxy)
	if status == http.StatusUnauthorized {
		if stateErr := p.refreshAPI.recordKimiFailure(ctx, latest, errKimiOAuthAccessTokenExpired); stateErr != nil {
			return nil, status, responseHeaders, stateErr
		}
	}
	return conn, status, responseHeaders, err
}
