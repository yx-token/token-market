// Package edgecfg 定义控制面与 Edge 之间的路由配置格式，以及在 Redis 中的存取方式。
//
// 控制面写入 tm:edge:key:<sha256(买家Key)>，并在 tm:edge:invalidate 频道广播变更；
// Edge 在内存中缓存配置，Redis 不可用时继续使用缓存。
package edgecfg

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	keyPrefix         = "tm:edge:key:"
	invalidateChannel = "tm:edge:invalidate"
	skipSetKey        = "tm:edge:skip"
	skipVersionKey    = "tm:edge:skip:version"
)

// Hop 是卖家链中的一跳。
type Hop struct {
	ListingID      int64  `json:"listing_id,omitempty"`
	PoolID         int64  `json:"pool_id,omitempty"`
	Label          string `json:"label"` // 写入响应头 X-TM-Listing
	InternalKeyID  int64  `json:"internal_key_id"`
	InternalKeyEnc []byte `json:"internal_key_enc"` // 用密钥环加密，AAD = InternalKeyAAD(InternalKeyID)
	// KeyAAD 不为空时用它作为密文的附加认证数据（第三方检测的临时令牌直接复用探测 key 的密文）
	KeyAAD string `json:"key_aad,omitempty"`
}

// AAD 返回内部 key 密文的附加认证数据。
func (h Hop) AAD() string {
	if h.KeyAAD != "" {
		return h.KeyAAD
	}
	return InternalKeyAAD(h.InternalKeyID)
}

// KeyConfig 是一个买家 Key 的路由配置。
type KeyConfig struct {
	BuyerKeyID      int64      `json:"buyer_key_id"`
	BuyerUserID     int64      `json:"buyer_user_id"`
	CreditCandidate bool       `json:"credit_candidate,omitempty"` // 本 Key 有可选店铺活动包时才查询活动运行时
	Status          string     `json:"status"`                     // active / disabled
	Reason          string     `json:"reason,omitempty"`           // 停用原因，返回给买家
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
	BlockedUntil    *time.Time `json:"blocked_until,omitempty"` // 花费限额触发的临时封禁
	IPWhitelist     []string   `json:"ip_whitelist,omitempty"`
	Hops            []Hop      `json:"hops"`
	Version         int64      `json:"version"`
}

// InternalKeyAAD 是内部 key 密文的附加认证数据。
func InternalKeyAAD(id int64) string { return "internal-key:" + strconv.FormatInt(id, 10) }

// CreditKeyAAD 与普通内部 Key 密文隔离，活动凭证不对外发布。
func CreditKeyAAD(id int64) string { return "store-credit-key:" + strconv.FormatInt(id, 10) }

// HashKey 计算买家 Key 的哈希（Key 本身是高熵随机串，无需加盐）。
func HashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

type Store struct {
	Redis *redis.Client
}

// Put 写入配置并广播。
func (s Store) Put(ctx context.Context, keyHash string, cfg KeyConfig) error {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := s.Redis.Set(ctx, keyPrefix+keyHash, raw, 0).Err(); err != nil {
		return err
	}
	return s.Redis.Publish(ctx, invalidateChannel, keyHash).Err()
}

// Delete 删除配置并广播。
func (s Store) Delete(ctx context.Context, keyHash string) error {
	if err := s.Redis.Del(ctx, keyPrefix+keyHash).Err(); err != nil {
		return err
	}
	return s.Redis.Publish(ctx, invalidateChannel, keyHash).Err()
}

var ErrNotFound = errors.New("edgecfg: key not found")

// Get 读取一个 Key 的配置。
func (s Store) Get(ctx context.Context, keyHash string) (*KeyConfig, error) {
	raw, err := s.Redis.Get(ctx, keyPrefix+keyHash).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var cfg KeyConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// LoadAll 全量读取（Edge 冷启动用）。
func (s Store) LoadAll(ctx context.Context) (map[string]*KeyConfig, error) {
	out := map[string]*KeyConfig{}
	iter := s.Redis.Scan(ctx, 0, keyPrefix+"*", 500).Iterator()
	for iter.Next(ctx) {
		k := iter.Val()
		raw, err := s.Redis.Get(ctx, k).Bytes()
		if errors.Is(err, redis.Nil) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var cfg KeyConfig
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return nil, err
		}
		out[k[len(keyPrefix):]] = &cfg
	}
	return out, iter.Err()
}

// SetSkipList 原子替换跳过列表（不应接流量的挂单），并递增版本号。
func (s Store) SetSkipList(ctx context.Context, listingIDs []int64) error {
	pipe := s.Redis.TxPipeline()
	pipe.Del(ctx, skipSetKey)
	if len(listingIDs) > 0 {
		members := make([]any, len(listingIDs))
		for i, id := range listingIDs {
			members[i] = id
		}
		pipe.SAdd(ctx, skipSetKey, members...)
	}
	pipe.Incr(ctx, skipVersionKey)
	_, err := pipe.Exec(ctx)
	if err != nil {
		return err
	}
	return s.Redis.Publish(ctx, invalidateChannel, "skip").Err()
}

// SkipList 读取跳过列表。
func (s Store) SkipList(ctx context.Context) (map[int64]bool, error) {
	members, err := s.Redis.SMembers(ctx, skipSetKey).Result()
	if err != nil {
		return nil, err
	}
	out := make(map[int64]bool, len(members))
	for _, m := range members {
		if id, err := strconv.ParseInt(m, 10, 64); err == nil {
			out[id] = true
		}
	}
	return out, nil
}

// Subscribe 订阅变更通知，返回的频道收到 key 哈希或 "skip"。
func (s Store) Subscribe(ctx context.Context) *redis.PubSub {
	return s.Redis.Subscribe(ctx, invalidateChannel)
}
