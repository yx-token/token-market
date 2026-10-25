package zhipu

import (
	"context"
	"crypto/sha256"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type SignatureMode string

const (
	SignatureAuto   SignatureMode = "auto"
	SignatureAlways SignatureMode = "always"
	SignatureNever  SignatureMode = "never"
)

type EndpointMapping struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type AgentConfig struct {
	CodingPlanSignature struct {
		Enable *bool `json:"enable"`
	} `json:"codingPlanSignature"`
	ProxyEndpoint struct {
		Mapping []EndpointMapping `json:"mapping"`
	} `json:"proxyEndpoint"`
}

func cloneConfig(config AgentConfig) AgentConfig {
	if config.CodingPlanSignature.Enable != nil {
		value := *config.CodingPlanSignature.Enable
		config.CodingPlanSignature.Enable = &value
	}
	config.ProxyEndpoint.Mapping = append([]EndpointMapping(nil), config.ProxyEndpoint.Mapping...)
	return config
}

// NormalizeEndpointMappings validates HTTPS endpoints and rejects duplicate canonical sources.
func NormalizeEndpointMappings(mappings []EndpointMapping) ([]EndpointMapping, error) {
	result := make([]EndpointMapping, 0, len(mappings))
	seen := make(map[string]bool)
	for _, mapping := range mappings {
		from, err := normalizeMappingURL(mapping.From)
		if err != nil {
			return nil, err
		}
		to, err := normalizeMappingURL(mapping.To)
		if err != nil {
			return nil, err
		}
		key := from.String()
		if seen[key] {
			return nil, errors.New("zhipu: duplicate normalized proxy mapping source")
		}
		seen[key] = true
		result = append(result, EndpointMapping{From: key, To: to.String()})
	}
	return result, nil
}

func normalizeMappingURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(raw, "#") {
		return nil, errors.New("zhipu: proxy mapping endpoints require HTTPS without userinfo, query or fragment")
	}
	host := strings.ToLower(u.Hostname())
	if port := u.Port(); port != "" && port != "443" {
		u.Host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		u.Host = "[" + host + "]"
	} else {
		u.Host = host
	}
	path := strings.TrimRight(u.EscapedPath(), "/")
	u.Path, err = url.PathUnescape(path)
	if err != nil {
		return nil, errors.New("zhipu: invalid proxy mapping path")
	}
	u.RawPath = path
	return u, nil
}

// MapEndpoint applies validated mappings after signing; the original query always wins.
func (c AgentConfig) MapEndpoint(original *url.URL) (*url.URL, error) {
	if original == nil {
		return nil, errors.New("zhipu: missing request URL")
	}
	result := *original
	mappings, err := NormalizeEndpointMappings(c.ProxyEndpoint.Mapping)
	if err != nil {
		return nil, err
	}
	lookup := result
	lookup.RawQuery, lookup.Fragment, lookup.RawFragment = "", "", ""
	lookup.ForceQuery = false
	key, err := normalizeMappingURL(lookup.String())
	if err != nil {
		return &result, nil
	}
	for _, mapping := range mappings {
		if mapping.From != key.String() {
			continue
		}
		to, err := url.Parse(mapping.To)
		if err != nil {
			return nil, err
		}
		to.RawQuery, to.ForceQuery = original.RawQuery, original.ForceQuery
		return to, nil
	}
	return &result, nil
}

type RemoteConfigOptions struct {
	// Headers corresponds to runtime codingPlanSignature.headers, merged with source
	// headers. X-Api-Key is always the call's credential; Authorization is never sent.
	Headers http.Header
}

type configEntry struct {
	config    AgentConfig
	err       error
	nextFetch time.Time
	inflight  chan struct{}
}

// RemoteConfigCache caches successful snapshots for an hour per credential. Failures
// have a one-minute backoff and no configuration snapshot. There is no background worker.
type RemoteConfigCache struct {
	client  *Client
	mu      sync.Mutex
	headers http.Header
	entries map[[32]byte]*configEntry
	now     func() time.Time
}

func NewRemoteConfigCache(client *Client, opts ...RemoteConfigOptions) *RemoteConfigCache {
	var headers http.Header
	if len(opts) > 0 {
		headers = make(http.Header)
		for name, values := range opts[0].Headers {
			for _, value := range values {
				headers.Add(name, value)
			}
		}
	}
	return &RemoteConfigCache{client: client, headers: headers, entries: make(map[[32]byte]*configEntry), now: time.Now}
}

func (c *RemoteConfigCache) Get(ctx context.Context, credential string) (AgentConfig, error) {
	if _, _, err := SplitCredential(credential); err != nil {
		return AgentConfig{}, err
	}
	hash := sha256.Sum256([]byte(credential))
	for {
		if err := ctx.Err(); err != nil {
			return AgentConfig{}, err
		}
		c.mu.Lock()
		entry := c.entries[hash]
		if entry == nil {
			if len(c.entries) >= maxCachedSigningKeys {
				for key, candidate := range c.entries {
					if candidate.inflight == nil {
						delete(c.entries, key)
						break
					}
				}
				if len(c.entries) >= maxCachedSigningKeys {
					c.mu.Unlock()
					return AgentConfig{}, errors.New("zhipu: too many concurrent config requests")
				}
			}
			entry = &configEntry{}
			c.entries[hash] = entry
		}
		if entry.inflight != nil {
			ready := entry.inflight
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				return AgentConfig{}, ctx.Err()
			case <-ready:
				continue
			}
		}
		if c.now().Before(entry.nextFetch) {
			config, err := cloneConfig(entry.config), entry.err
			c.mu.Unlock()
			return config, err
		}
		entry.inflight = make(chan struct{})
		c.mu.Unlock()
		var config AgentConfig
		q := url.Values{"app_version": {c.client.version}, "platform": {SourceHeaders(c.client.source).Get("X-Platform")}}
		headers := c.headers.Clone()
		if headers == nil {
			headers = make(http.Header)
		}
		headers.Del("Authorization")
		headers.Set("X-Api-Key", credential)
		err := c.client.doWithHeaders(ctx, http.MethodGet, c.client.zcode+"/api/v1/agent/configs?"+q.Encode(), headers, nil, "0", &config)
		if err == nil {
			config.ProxyEndpoint.Mapping, err = NormalizeEndpointMappings(config.ProxyEndpoint.Mapping)
		}
		c.mu.Lock()
		if err == nil {
			entry.config = cloneConfig(config)
			entry.nextFetch = c.now().Add(time.Hour)
		} else {
			entry.config = AgentConfig{}
			entry.nextFetch = c.now().Add(time.Minute)
			if ctx.Err() != nil {
				entry.nextFetch = c.now()
			}
		}
		entry.err = err
		close(entry.inflight)
		entry.inflight = nil
		result := cloneConfig(entry.config)
		c.mu.Unlock()
		return result, err
	}
}
