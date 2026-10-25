package zhipu

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/hkdf"
)

const (
	SigningKDFSalt       = "WD_CLIENT_SIGN_KDF_SALT"
	SigningAppID         = "zcode"
	clientKeyPath        = "/api/paas/c1f3a7e2/v2/client"
	maxCachedSigningKeys = 256
	SignatureBypassTTL   = 10 * time.Minute
)

type SignerOptions struct {
	// Origin is the original business origin, before proxy endpoint routing.
	Origin            string
	Mode              SignatureMode // Empty defaults to always; auto treats config failures as disabled.
	Config            *RemoteConfigCache
	ConfigHeaders     http.Header // codingPlanSignature.headers; X-Api-Key is supplied per credential.
	AllowInsecureHTTP bool        // Test-only; production signing/handshakes require HTTPS.
}

type signingEntry struct {
	ready chan struct{}
	key   ed25519.PrivateKey
	err   error
}

// Signer caches decrypted private keys only in memory, keyed by credential+origin.
// Use one instance per outbound proxy policy and share it between concurrent requests.
type Signer struct {
	client *Client
	origin string
	mode   SignatureMode
	config *RemoteConfigCache
	mu     sync.Mutex
	keys   map[[32]byte]*signingEntry
	bypass map[[32]byte]time.Time
	now    func() time.Time
}

func NewSigner(client *Client, opts SignerOptions) (*Signer, error) {
	if client == nil {
		return nil, errors.New("zhipu: missing signing HTTP client")
	}
	origin, err := parseOrigin(opts.Origin, opts.AllowInsecureHTTP)
	if err != nil {
		return nil, err
	}
	if opts.Mode == "" {
		opts.Mode = SignatureAlways
	}
	if opts.Mode != SignatureAlways && opts.Mode != SignatureAuto && opts.Mode != SignatureNever {
		return nil, errors.New("zhipu: invalid signature mode")
	}
	if opts.Config == nil {
		opts.Config = NewRemoteConfigCache(client, RemoteConfigOptions{Headers: opts.ConfigHeaders})
	}
	return &Signer{client: client, origin: origin, mode: opts.Mode, config: opts.Config, keys: make(map[[32]byte]*signingEntry), bypass: make(map[[32]byte]time.Time), now: time.Now}, nil
}

func deriveBytes(secret, info string) ([]byte, error) {
	key := make([]byte, 32)
	if _, err := io.ReadFull(hkdf.New(sha256.New, []byte(secret), []byte(SigningKDFSalt), []byte(info)), key); err != nil {
		return nil, fmt.Errorf("zhipu HKDF: %w", err)
	}
	return key, nil
}

func handshakeSignature(id, secret, ts, nonce string) (string, error) {
	key, err := deriveBytes(secret, "getSignKey_hmac")
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("get_sign_key\n" + id + "\n" + ts + "\n" + nonce))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil)), nil
}

func decryptPrivateKey(privateCipher, id, secret string) (ed25519.PrivateKey, error) {
	packed, err := base64.StdEncoding.DecodeString(privateCipher)
	if err != nil {
		return nil, errors.New("zhipu: invalid privateCipher encoding")
	}
	key, err := deriveBytes(secret, "ed25519_priv")
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("zhipu AES: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("zhipu GCM: %w", err)
	}
	if len(packed) < gcm.NonceSize()+gcm.Overhead() {
		return nil, errors.New("zhipu: privateCipher is too short")
	}
	plaintext, err := gcm.Open(nil, packed[:gcm.NonceSize()], packed[gcm.NonceSize():], []byte(id))
	if err != nil {
		return nil, errors.New("zhipu: privateCipher authentication failed")
	}
	der, err := base64.StdEncoding.DecodeString(string(plaintext))
	if err != nil {
		return nil, errors.New("zhipu: invalid private key encoding")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, errors.New("zhipu: invalid PKCS8 private key")
	}
	privateKey, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("zhipu: handshake key is not Ed25519")
	}
	return privateKey, nil
}

func (s *Signer) handshake(ctx context.Context, credential string) (ed25519.PrivateKey, error) {
	id, secret, err := SplitCredential(credential)
	if err != nil {
		return nil, err
	}
	ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
	nonce, err := randomHex(16)
	if err != nil {
		return nil, err
	}
	sig, err := handshakeSignature(id, secret, ts, nonce)
	if err != nil {
		return nil, err
	}
	var data struct {
		PrivateCipher string `json:"privateCipher"`
	}
	if err := s.client.do(ctx, http.MethodPost, s.origin+clientKeyPath, credential, map[string]string{"apiKey": credential, "nonce": nonce, "sig": sig, "ts": ts}, "200", &data); err != nil {
		return nil, err
	}
	return decryptPrivateKey(data.PrivateCipher, id, secret)
}

func (s *Signer) cacheKey(credential string) [32]byte {
	return sha256.Sum256([]byte(credential + "\x00" + s.origin))
}

func (s *Signer) privateKey(ctx context.Context, credential string) (ed25519.PrivateKey, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	hash := s.cacheKey(credential)
	s.mu.Lock()
	if entry := s.keys[hash]; entry != nil {
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-entry.ready:
			return entry.key, entry.err
		}
	}
	if len(s.keys) >= maxCachedSigningKeys {
		// Eviction affects only reuse; active requests retain their key references.
		for hash, entry := range s.keys {
			select {
			case <-entry.ready:
				delete(s.keys, hash)
			default:
			}
			if len(s.keys) < maxCachedSigningKeys {
				break
			}
		}
		if len(s.keys) >= maxCachedSigningKeys {
			s.mu.Unlock()
			return nil, errors.New("zhipu: too many concurrent signing handshakes")
		}
	}
	entry := &signingEntry{ready: make(chan struct{})}
	s.keys[hash] = entry
	s.mu.Unlock()
	key, err := s.handshake(ctx, credential)
	s.mu.Lock()
	entry.key, entry.err = key, err
	close(entry.ready)
	if err != nil && s.keys[hash] == entry {
		delete(s.keys, hash)
	}
	s.mu.Unlock()
	return key, err
}

func (s *Signer) Invalidate(credential string) {
	s.mu.Lock()
	delete(s.keys, s.cacheKey(credential))
	delete(s.bypass, s.cacheKey(credential))
	s.mu.Unlock()
}

// A delayed rejection from an older key must not evict a concurrently refreshed key.
func (s *Signer) invalidateForRequest(credential string, headers http.Header) {
	id, _, err := SplitCredential(credential)
	if err != nil {
		return
	}
	sig, err := base64.StdEncoding.DecodeString(headers.Get("X-Client-Sig"))
	if err != nil {
		return
	}
	message := signatureMessage(id, headers.Get("X-Client-Ts"), headers.Get("X-Client-Version"), headers.Get("X-Session-Id"), headers.Get("X-Client-Nonce"))
	s.mu.Lock()
	defer s.mu.Unlock()
	hash := s.cacheKey(credential)
	entry := s.keys[hash]
	if entry == nil {
		return
	}
	select {
	case <-entry.ready:
	default:
		return
	}
	if len(entry.key) == ed25519.PrivateKeySize && ed25519.Verify(ed25519.PublicKey(entry.key[32:]), []byte(message), sig) {
		delete(s.keys, hash)
	}
}

func (s *Signer) Clear() { s.mu.Lock(); clear(s.keys); clear(s.bypass); s.mu.Unlock() }

// Bypass discards the key and temporarily sends unsigned requests for this credential/origin.
// It is used only after two signature rejections; Invalidate or Clear ends it early.
func (s *Signer) Bypass(credential string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	hash := s.cacheKey(credential)
	delete(s.keys, hash)
	for key, until := range s.bypass {
		if !s.now().Before(until) {
			delete(s.bypass, key)
		}
	}
	if len(s.bypass) >= maxCachedSigningKeys {
		for key := range s.bypass {
			delete(s.bypass, key)
			break
		}
	}
	s.bypass[hash] = s.now().Add(SignatureBypassTTL)
}

func (s *Signer) bypassing(credential string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	hash := s.cacheKey(credential)
	until, ok := s.bypass[hash]
	if ok && !s.now().Before(until) {
		delete(s.bypass, hash)
		return false
	}
	return ok
}

func clearSigningHeaders(headers http.Header) {
	// Header maps can be populated directly with noncanonical casing by callers.
	for name := range headers {
		switch strings.ToLower(name) {
		case "x-client-ts", "x-client-version", "x-client-sig", "x-client-nonce", "x-client-pow", "x-app-id", "x-client-sign-verified":
			delete(headers, name)
		}
	}
}

func IsUnsignedPath(path string) bool {
	switch strings.TrimRight(path, "/") {
	case "/api/v1/zcode-plan/anthropic/v1/messages", "/api/v1/zcode-plan/chat/completions", "/api/v1/off-peak/anthropic/v1/messages":
		return true
	default:
		return false
	}
}

func signatureMessage(id, ts, version, sessionID, nonce string) string {
	return strings.Join([]string{id, ts, version, sessionID, nonce}, "\n")
}

func powSeed(id, sessionID, ts string) string {
	digest := sha256.Sum256([]byte(id + "\n" + SigningAppID + "\n" + sessionID + "\n" + ts))
	return hex.EncodeToString(digest[:])[:32]
}

func computePoW(ctx context.Context, seed, prefix string) (string, error) {
	return computePoWBits(ctx, seed, prefix, 8)
}

func computePoWBits(ctx context.Context, seed, prefix string, bits int) (string, error) {
	if bits < 0 || bits > 32 {
		return "", errors.New("powBits must be an integer between 0 and 32")
	}
	for counter := uint64(0); counter <= uint64(^uint32(0)); counter++ {
		if counter%256 == 0 {
			if err := ctx.Err(); err != nil {
				return "", err
			}
		}
		candidate := prefix + fmt.Sprintf("%08x", counter)
		digest := sha256.Sum256([]byte(seed + "\n" + candidate))
		if hasLeadingZeroBits(digest, bits) {
			return candidate, nil
		}
	}
	return "", errors.New("zhipu: PoW counter exhausted")
}

func hasLeadingZeroBits(digest [32]byte, bits int) bool {
	for i := 0; i < bits/8; i++ {
		if digest[i] != 0 {
			return false
		}
	}
	remaining := bits % 8
	return remaining == 0 || digest[bits/8]>>(8-remaining) == 0
}

// Sign adds source and V4 headers in place without reading/modifying the request body.
// Session IDs belong to the end user's inference session, not the OAuth flow.
// Sign cannot observe a response: use SigningTransport for re-handshake and temporary
// unsigned fallback, or implement the same bounded response handling at the call site.
func (s *Signer) Sign(ctx context.Context, req *http.Request, credential, sessionID string) error {
	if req == nil || req.URL == nil {
		return errors.New("zhipu: missing signing request")
	}
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	clearSigningHeaders(req.Header)
	if err := ctx.Err(); err != nil {
		return err
	}
	for name, values := range SourceHeaders(s.client.source) {
		req.Header[name] = values
	}
	if req.URL.Scheme+"://"+req.URL.Host != s.origin || s.mode == SignatureNever || IsUnsignedPath(req.URL.Path) || s.bypassing(credential) {
		return nil
	}
	if s.mode == SignatureAuto {
		config, err := s.config.Get(ctx, credential)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return nil
		}
		if config.CodingPlanSignature.Enable == nil || !*config.CodingPlanSignature.Enable {
			return nil
		}
	}
	if strings.TrimSpace(sessionID) == "" || strings.ContainsAny(sessionID, "\r\n") {
		return errors.New("zhipu: nonempty session ID required")
	}
	id, _, err := SplitCredential(credential)
	if err != nil {
		return err
	}
	key, err := s.privateKey(ctx, credential)
	if err != nil {
		return err
	}
	ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
	nonce, err := randomHex(16)
	if err != nil {
		return err
	}
	prefix, err := randomHex(12)
	if err != nil {
		return err
	}
	pow, err := computePoW(ctx, powSeed(id, sessionID, ts), prefix)
	if err != nil {
		return err
	}
	sig := ed25519.Sign(key, []byte(signatureMessage(id, ts, s.client.version, sessionID, nonce)))
	for name, value := range map[string]string{
		"X-Client-Ts": ts, "X-Client-Version": s.client.version, "X-Client-Sig": base64.StdEncoding.EncodeToString(sig),
		"X-Client-Nonce": nonce, "X-Client-Pow": pow, "X-App-Id": SigningAppID, "X-Session-Id": sessionID,
	} {
		req.Header.Set(name, value)
	}
	return nil
}
