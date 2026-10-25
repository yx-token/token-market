//go:build unit

package zhipu

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

func TestRemoteConfigCredentialIsolationAndHeaderSnapshot(t *testing.T) {
	var calls atomic.Int64
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		credential := r.Header.Get("X-Api-Key")
		if r.Header.Get("Authorization") != "" || r.Header.Get("X-Extra") != "original" || r.Header.Get("User-Agent") != "ConfiguredZCode" {
			t.Error("config headers were not safely merged/snapshotted")
		}
		if credential != "a.secret" && credential != "b.secret" {
			t.Error("config request missing full credential")
		}
		respond(t, w, 0, map[string]any{"codingPlanSignature": map[string]bool{"enable": credential == "a.secret"}})
	})
	headers := http.Header{"X-Extra": {"original"}, "user-agent": {"ConfiguredZCode"}, "authorization": {"do-not-send"}, "x-api-key": {"override-me"}}
	cache := NewRemoteConfigCache(c, RemoteConfigOptions{Headers: headers})
	headers["X-Extra"][0] = "mutated"
	headers.Set("User-Agent", "mutated")
	for range 2 {
		for _, credential := range []string{"a.secret", "b.secret"} {
			cfg, err := cache.Get(context.Background(), credential)
			if err != nil || cfg.CodingPlanSignature.Enable == nil || *cfg.CodingPlanSignature.Enable != (credential == "a.secret") {
				t.Fatalf("credential cache leaked: %s %v", credential, err)
			}
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("want one fetch per credential, got %d", calls.Load())
	}
}

func TestMappingValidationAndNormalization(t *testing.T) {
	valid := "https://source.invalid/path"
	for _, bad := range []string{"http://source.invalid/path", "https://user@source.invalid/path", "https://source.invalid/path?q=1", "https://source.invalid/path?", "https://source.invalid/path#fragment", "https://source.invalid/path#"} {
		for _, side := range []string{"from", "to"} {
			mapping := EndpointMapping{From: valid, To: valid}
			if side == "from" {
				mapping.From = bad
			} else {
				mapping.To = bad
			}
			if _, err := NormalizeEndpointMappings([]EndpointMapping{mapping}); err == nil {
				t.Errorf("invalid %s accepted: %s", side, bad)
			}
		}
	}
	for _, duplicate := range []string{"https://source.invalid/path/", "https://SOURCE.INVALID:443/path///"} {
		if _, err := NormalizeEndpointMappings([]EndpointMapping{{From: valid, To: valid}, {From: duplicate, To: valid}}); err == nil {
			t.Errorf("duplicate normalized source accepted: %s", duplicate)
		}
	}
	var config AgentConfig
	config.ProxyEndpoint.Mapping = []EndpointMapping{{From: "https://SOURCE.INVALID:443/path///", To: "https://TARGET.INVALID:443/target///"}}
	for _, query := range []string{"?q=a%2Bb&q=c", "?", ""} {
		original, err := url.Parse(valid + "/" + query)
		if err != nil {
			t.Fatal(err)
		}
		mapped, err := config.MapEndpoint(original)
		if err != nil || mapped.String() != "https://target.invalid/target"+query || original.String() != valid+"/"+query {
			t.Fatalf("mapping changed query or input: mapped=%v err=%v", mapped, err)
		}
	}
}

func TestRoutingConfigFailurePreservesSignedRequest(t *testing.T) {
	remote, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) })
	c, server, fixture := newSigningServer(t)
	signer := testSigner(t, c, server.URL, SignatureAlways, nil)
	transport := &SigningTransport{Signer: signer, Credential: "key-id.secret", SessionID: "end-user-session", Base: &RoutingTransport{Config: NewRemoteConfigCache(remote), Credential: "key-id.secret", Base: server.Client().Transport}}
	resp, err := transport.RoundTrip(signingRequest(t, server))
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if err != nil || resp.StatusCode != 200 || fixture.handshakes.Load() != 1 || fixture.unsignedRequests.Load() != 0 {
		t.Fatalf("remote outage affected always signature: %v", err)
	}
}

func TestSourceTimezoneAndConfiguredOrigin(t *testing.T) {
	for _, tc := range []struct {
		location *time.Location
		want     string
	}{{nil, "Asia/Shanghai"}, {time.FixedZone("Local", 0), "Asia/Shanghai"}, {time.FixedZone("invalid-zone", 0), "Asia/Shanghai"}, {time.FixedZone("Asia/Singapore", 8*3600), "Asia/Singapore"}, {time.UTC, "UTC"}} {
		if got := defaultTimezone(tc.location); got != tc.want {
			t.Errorf("timezone = %q, want %q", got, tc.want)
		}
	}
	var referer string
	c, server := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		referer = r.Header.Get("HTTP-Referer")
		respond(t, w, 0, map[string]string{"id": "u"})
	})
	if _, err := c.UserInfo(context.Background(), ProviderBigModel, "oauth"); err != nil {
		t.Fatal(err)
	}
	if referer != server.URL {
		t.Fatalf("referer did not follow configured zcode origin: %q", referer)
	}
	headers := SourceHeaders(SourceOptions{Timezone: "Europe/Paris", OSVersion: "custom-os", ZCodeOrigin: "https://custom.invalid"})
	if headers.Get("X-Client-Timezone") != "Europe/Paris" || headers.Get("X-Os-Version") != "custom-os" || headers.Get("HTTP-Referer") != "https://custom.invalid" {
		t.Fatal("source overrides lost")
	}
}
