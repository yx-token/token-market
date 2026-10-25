package zhipu

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	ErrSessionExpired = errors.New("zhipu: OAuth session expired")
	ErrSessionBusy    = errors.New("zhipu: OAuth session is being processed")
)

// OAuthSession is ephemeral Redis state. Owner claims serialize poll and manual
// exchange across instances; a crashed owner cannot exchange a one-use code twice.
type OAuthSession struct {
	Provider    Provider        `json:"provider"`
	FlowID      string          `json:"flow_id,omitempty"`
	State       string          `json:"state,omitempty"`
	PollToken   string          `json:"poll_token,omitempty"`
	ProxyID     *int64          `json:"proxy_id,omitempty"`
	ProxyURL    string          `json:"proxy_url,omitempty"`
	RedirectURI string          `json:"redirect_uri,omitempty"`
	ExpiresAt   time.Time       `json:"expires_at"`
	Interval    int             `json:"interval"`
	NextPollAt  time.Time       `json:"next_poll_at"`
	Owner       string          `json:"owner,omitempty"`
	Status      string          `json:"status"`
	Result      json.RawMessage `json:"result,omitempty"`
}

type OAuthSessionStore interface {
	Create(context.Context, string, *OAuthSession) error
	Acquire(context.Context, string, string) (*OAuthSession, error)
	Save(context.Context, string, string, *OAuthSession) error
}

type RedisOAuthSessionStore struct{ client *redis.Client }

func NewRedisOAuthSessionStore(client *redis.Client) *RedisOAuthSessionStore {
	return &RedisOAuthSessionStore{client: client}
}

func (s *RedisOAuthSessionStore) Create(ctx context.Context, id string, session *OAuthSession) error {
	if s == nil || s.client == nil {
		return errors.New("zhipu: Redis session store is not configured")
	}
	ttl := time.Until(session.ExpiresAt)
	if ttl <= 0 {
		return ErrSessionExpired
	}
	raw, err := json.Marshal(session)
	if err != nil {
		return err
	}
	ok, err := s.client.SetNX(ctx, "zhipu:oauth:session:"+id, raw, ttl).Result()
	if err == nil && !ok {
		return ErrSessionBusy
	}
	return err
}

func (s *RedisOAuthSessionStore) Acquire(ctx context.Context, id, owner string) (*OAuthSession, error) {
	if s == nil || s.client == nil {
		return nil, errors.New("zhipu: Redis session store is not configured")
	}
	raw, err := s.client.Eval(ctx, `
local raw = redis.call('GET', KEYS[1])
if not raw then return '' end
local value = cjson.decode(raw)
if value.status ~= 'pending' then return raw end
if value.owner and value.owner ~= '' then return 'busy' end
value.owner = ARGV[1]
redis.call('SET', KEYS[1], cjson.encode(value), 'KEEPTTL')
return raw`, []string{"zhipu:oauth:session:" + id}, owner).Text()
	if err != nil {
		return nil, err
	}
	if raw == "" {
		return nil, ErrSessionExpired
	}
	if raw == "busy" {
		return nil, ErrSessionBusy
	}
	var session OAuthSession
	if err := json.Unmarshal([]byte(raw), &session); err != nil {
		return nil, err
	}
	return &session, nil
}

func (s *RedisOAuthSessionStore) Save(ctx context.Context, id, owner string, session *OAuthSession) error {
	session.Owner = ""
	raw, err := json.Marshal(session)
	if err != nil {
		return err
	}
	ok, err := s.client.Eval(ctx, `
local raw = redis.call('GET', KEYS[1])
if not raw then return 0 end
local value = cjson.decode(raw)
if value.owner ~= ARGV[1] then return 0 end
redis.call('SET', KEYS[1], ARGV[2], 'KEEPTTL')
return 1`, []string{"zhipu:oauth:session:" + id}, owner, string(raw)).Int()
	if err == nil && ok != 1 {
		return ErrSessionExpired
	}
	return err
}
