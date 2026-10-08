package buyer

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/DueGin/token-market/internal/edgecfg"
	"github.com/DueGin/token-market/internal/listing"
	"github.com/DueGin/token-market/internal/platform/crypto"
	"github.com/DueGin/token-market/internal/platform/httpx"
	"github.com/DueGin/token-market/internal/scheduling"
	"github.com/DueGin/token-market/internal/settings"
	"github.com/DueGin/token-market/internal/sub2api/provision"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maxHops = 5

type HopInput struct {
	ListingID int64  `json:"listing_id,omitempty"`
	Pool      string `json:"pool,omitempty"` // 默认分组名称，只能出现在最后一跳
}

type RouteInput struct {
	Type string     `json:"type"` // default / chain
	Pool string     `json:"pool,omitempty"`
	Hops []HopInput `json:"hops,omitempty"`
}

// Limits 是买家 Key 级别的限制。花费类限制由控制面按用量统计后执行，有几分钟延迟。
type Limits struct {
	QuotaUSD    *float64   `json:"quota_usd,omitempty"`
	RateLimit5h *float64   `json:"rate_limit_5h_usd,omitempty"`
	RateLimit1d *float64   `json:"rate_limit_1d_usd,omitempty"`
	RateLimit7d *float64   `json:"rate_limit_7d_usd,omitempty"`
	IPWhitelist []string   `json:"ip_whitelist,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

func (l Limits) validate() error {
	for _, v := range []*float64{l.QuotaUSD, l.RateLimit5h, l.RateLimit1d, l.RateLimit7d} {
		if v != nil && *v <= 0 {
			return httpx.BadRequest("INVALID_LIMIT", "spending limits must be > 0")
		}
	}
	for _, ip := range l.IPWhitelist {
		if net.ParseIP(ip) == nil {
			if _, _, err := net.ParseCIDR(ip); err != nil {
				return httpx.BadRequest("INVALID_IP", "invalid ip or cidr: "+ip)
			}
		}
	}
	return nil
}

type KeyInput struct {
	Name   string     `json:"name"`
	Route  RouteInput `json:"route"`
	Limits Limits     `json:"limits"`
}

type HopView struct {
	Position  int    `json:"position"`
	ListingID int64  `json:"listing_id,omitempty"`
	Pool      string `json:"pool,omitempty"`
	State     string `json:"state"` // ready / provisioning / skipped
}

type BuyerKey struct {
	ID                int64      `json:"id"`
	Name              string     `json:"name"`
	KeyPrefix         string     `json:"key_prefix"`
	RouteType         string     `json:"route_type"`
	Status            string     `json:"status"`
	DisabledReason    *string    `json:"disabled_reason,omitempty"`
	Limits            Limits     `json:"limits"`
	LimitBlockedUntil *time.Time `json:"limit_blocked_until,omitempty"`
	LimitBlockReason  *string    `json:"limit_block_reason,omitempty"`
	Hops              []HopView  `json:"hops"`
	CreatedAt         time.Time  `json:"created_at"`
}

type KeyService struct {
	DB       *pgxpool.Pool
	Shadow   *ShadowService
	Prov     *provision.Provisioner
	Keyring  *crypto.Keyring
	Edge     edgecfg.Store
	Kicker   listing.Kicker
	Settings func(ctx context.Context) (settings.Settings, error)
	Log      *slog.Logger
}

type resolvedHop struct {
	listingID int64
	poolID    int64
	poolName  string
	platform  string
}

// validateRoute 校验路由：最多 5 跳、默认分组只能在最后、同一挂单不重复、各跳同一平台。
func (s *KeyService) validateRoute(ctx context.Context, r RouteInput) (string, []resolvedHop, error) {
	switch r.Type {
	case "default":
		if r.Pool == "" || len(r.Hops) > 0 {
			return "", nil, httpx.BadRequest("INVALID_ROUTE", "default route needs exactly one pool")
		}
		p, err := scheduling.GetPoolByName(ctx, s.DB, r.Pool)
		if err != nil {
			return "", nil, err
		}
		if !p.Enabled || p.Sub2APIGroupID == nil {
			return "", nil, httpx.BadRequest("POOL_UNAVAILABLE", "pool is not available")
		}
		return "default", []resolvedHop{{poolID: p.ID, poolName: p.Name, platform: p.Platform}}, nil
	case "chain":
	default:
		return "", nil, httpx.BadRequest("INVALID_ROUTE", "route type must be default or chain")
	}
	if len(r.Hops) == 0 || len(r.Hops) > maxHops {
		return "", nil, httpx.BadRequest("INVALID_ROUTE", fmt.Sprintf("chain must have 1 to %d hops", maxHops))
	}
	var hops []resolvedHop
	seen := map[int64]bool{}
	platform := ""
	for i, h := range r.Hops {
		switch {
		case h.ListingID > 0 && h.Pool == "":
			if seen[h.ListingID] {
				return "", nil, httpx.BadRequest("DUPLICATE_HOP", "a listing can appear only once")
			}
			seen[h.ListingID] = true
			l, err := listing.GetListing(ctx, s.DB, h.ListingID)
			if err != nil {
				return "", nil, err
			}
			if l.Mode != listing.ModeConsignment || l.Status != listing.ListingActive {
				return "", nil, httpx.BadRequest("LISTING_UNAVAILABLE", fmt.Sprintf("listing %d is not an active consignment listing", l.ID))
			}
			u, err := listing.GetUpstream(ctx, s.DB, l.UpstreamID)
			if err != nil {
				return "", nil, err
			}
			if platform != "" && u.Platform != platform {
				return "", nil, httpx.BadRequest("MIXED_PLATFORM", "all hops in a chain must be the same platform")
			}
			platform = u.Platform
			hops = append(hops, resolvedHop{listingID: l.ID, platform: u.Platform})
		case h.Pool != "" && h.ListingID == 0:
			if i != len(r.Hops)-1 {
				return "", nil, httpx.BadRequest("POOL_NOT_LAST", "a default pool can only be the last hop")
			}
			p, err := scheduling.GetPoolByName(ctx, s.DB, h.Pool)
			if err != nil {
				return "", nil, err
			}
			if !p.Enabled || p.Sub2APIGroupID == nil {
				return "", nil, httpx.BadRequest("POOL_UNAVAILABLE", "pool is not available")
			}
			if platform != "" && p.Platform != platform && p.Platform != "composite" {
				return "", nil, httpx.BadRequest("MIXED_PLATFORM", "fallback pool must match the chain platform or be the composite pool")
			}
			hops = append(hops, resolvedHop{poolID: p.ID, poolName: p.Name, platform: p.Platform})
		default:
			return "", nil, httpx.BadRequest("INVALID_HOP", "each hop needs either listing_id or pool")
		}
	}
	return "chain", hops, nil
}

// Create 创建买家 Key。明文只在这里返回一次，数据库只存哈希。
func (s *KeyService) Create(ctx context.Context, userID int64, in KeyInput) (string, *BuyerKey, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 100 {
		return "", nil, httpx.BadRequest("INVALID_NAME", "name is required (max 100 chars)")
	}
	if err := in.Limits.validate(); err != nil {
		return "", nil, err
	}
	routeType, hops, err := s.validateRoute(ctx, in.Route)
	if err != nil {
		return "", nil, err
	}
	plain := "tm-" + randomString(40)
	hash := sha256.Sum256([]byte(plain))
	limits, _ := json.Marshal(in.Limits)

	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id int64
	if err := tx.QueryRow(ctx, `INSERT INTO buyer_keys (user_id, name, key_hash, key_prefix, route_type, limits)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`, userID, in.Name, hash[:], plain[:10], routeType, limits).Scan(&id); err != nil {
		return "", nil, err
	}
	if err := insertHops(ctx, tx, id, hops); err != nil {
		return "", nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", nil, err
	}
	// 同步开通；失败时 Key 保持 provisioning，由后台任务重试
	if err := s.Provision(ctx, id); err != nil {
		s.Log.Warn("provision buyer key", "key_id", id, "err", err)
	}
	k, err := s.Get(ctx, userID, id)
	return plain, k, err
}

func insertHops(ctx context.Context, tx pgx.Tx, keyID int64, hops []resolvedHop) error {
	if _, err := tx.Exec(ctx, "DELETE FROM buyer_key_hops WHERE key_id = $1", keyID); err != nil {
		return err
	}
	for i, h := range hops {
		var lid, pid *int64
		if h.listingID > 0 {
			lid = &h.listingID
		} else {
			pid = &h.poolID
		}
		if _, err := tx.Exec(ctx, `INSERT INTO buyer_key_hops (key_id, position, listing_id, pool_id) VALUES ($1, $2, $3, $4)`,
			keyID, i+1, lid, pid); err != nil {
			return err
		}
	}
	return nil
}

// Provision 为 Key 的每一跳准备 sub2api 资源（影子用户、可用分组、内部 key），然后发布路由配置。幂等。
func (s *KeyService) Provision(ctx context.Context, keyID int64) error {
	var userID int64
	var status string
	if err := s.DB.QueryRow(ctx, "SELECT user_id, status FROM buyer_keys WHERE id = $1", keyID).Scan(&userID, &status); err != nil {
		return err
	}
	if status == "deleted" {
		return nil
	}
	it, err := s.Shadow.Ensure(ctx, KindBuyer, userID)
	if err != nil {
		return err
	}
	hops, err := s.loadHops(ctx, keyID)
	if err != nil {
		return err
	}
	for _, h := range hops {
		groupID, err := s.hopGroup(ctx, h)
		if err != nil {
			return err
		}
		if err := s.Shadow.AllowGroup(ctx, it.ID, groupID); err != nil {
			return err
		}
		if err := s.ensureInternalKey(ctx, it, keyID, userID, groupID); err != nil {
			return err
		}
	}
	if status == "provisioning" {
		if _, err := s.DB.Exec(ctx, "UPDATE buyer_keys SET status = 'active', updated_at = now() WHERE id = $1 AND status = 'provisioning'", keyID); err != nil {
			return err
		}
	}
	return s.Publish(ctx, keyID)
}

type hopRow struct {
	position  int
	listingID *int64
	poolID    *int64
}

func (s *KeyService) loadHops(ctx context.Context, keyID int64) ([]hopRow, error) {
	rows, err := s.DB.Query(ctx, "SELECT position, listing_id, pool_id FROM buyer_key_hops WHERE key_id = $1 ORDER BY position", keyID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (hopRow, error) {
		var h hopRow
		err := row.Scan(&h.position, &h.listingID, &h.poolID)
		return h, err
	})
}

// hopGroup 返回一跳对应的 sub2api 分组；寄售挂单的专属分组在第一次被指定时创建。
func (s *KeyService) hopGroup(ctx context.Context, h hopRow) (int64, error) {
	if h.poolID != nil {
		p, err := scheduling.GetPool(ctx, s.DB, *h.poolID)
		if err != nil {
			return 0, err
		}
		if p.Sub2APIGroupID == nil {
			return 0, errors.New("pool has no sub2api group yet")
		}
		return *p.Sub2APIGroupID, nil
	}
	return s.EnsureListingGroup(ctx, *h.listingID)
}

// EnsureListingGroup 确保寄售挂单的专属分组存在，倍率为买家价 p = m_s × (1 + c)。
func (s *KeyService) EnsureListingGroup(ctx context.Context, listingID int64) (int64, error) {
	l, err := listing.GetListing(ctx, s.DB, listingID)
	if err != nil {
		return 0, err
	}
	if l.ListingGroupID != nil {
		return *l.ListingGroupID, nil
	}
	if l.Multiplier == nil {
		return 0, errors.New("listing has no price")
	}
	u, err := listing.GetUpstream(ctx, s.DB, l.UpstreamID)
	if err != nil {
		return 0, err
	}
	st, err := s.Settings(ctx)
	if err != nil {
		return 0, err
	}
	rate := listing.BuyerPrice(*l.Multiplier, st.CommissionRate)
	gid, err := s.Prov.EnsureGroup(ctx, provision.GroupSpec{
		Name:           listing.ListingGroupName(l.ID),
		Platform:       u.Platform,
		RateMultiplier: rate,
		Exclusive:      true,
		Description:    "token-market listing group",
	})
	if err != nil {
		return 0, err
	}
	if _, err := s.DB.Exec(ctx, "UPDATE listings SET listing_group_id = $2, listing_group_rate = $3, updated_at = now() WHERE id = $1",
		l.ID, gid, rate); err != nil {
		return 0, err
	}
	if s.Kicker != nil {
		s.Kicker.Kick(ctx) // 让调度器把卖家账号加入专属分组
	}
	return gid, nil
}

func (s *KeyService) ensureInternalKey(ctx context.Context, it *Identity, keyID, userID, groupID int64) error {
	var id int64
	var subKeyID *int64
	var enc []byte
	err := s.DB.QueryRow(ctx, "SELECT id, sub2api_key_id, key_enc FROM internal_keys WHERE buyer_key_id = $1 AND sub2api_group_id = $2",
		keyID, groupID).Scan(&id, &subKeyID, &enc)
	if errors.Is(err, pgx.ErrNoRows) {
		// 先落库再调用 sub2api，重试时沿用同一个 custom_key
		plain := "tmik_" + randomString(40)
		h := sha256.Sum256([]byte(plain))
		if err := s.DB.QueryRow(ctx, `INSERT INTO internal_keys (buyer_key_id, buyer_user_id, sub2api_group_id, key_enc, key_hash)
			VALUES ($1, $2, $3, '\x00', $4) RETURNING id`, keyID, userID, groupID, h[:]).Scan(&id); err != nil {
			return err
		}
		if enc, err = s.Keyring.SealString(plain, edgecfg.InternalKeyAAD(id)); err != nil {
			return err
		}
		if _, err := s.DB.Exec(ctx, "UPDATE internal_keys SET key_enc = $2 WHERE id = $1", id, enc); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if subKeyID != nil {
		return nil
	}
	if len(enc) <= 1 {
		// 上次在写入密文前中断：重新生成（尚未在 sub2api 创建，换 key 是安全的）
		plain := "tmik_" + randomString(40)
		h := sha256.Sum256([]byte(plain))
		if enc, err = s.Keyring.SealString(plain, edgecfg.InternalKeyAAD(id)); err != nil {
			return err
		}
		if _, err := s.DB.Exec(ctx, "UPDATE internal_keys SET key_enc = $2, key_hash = $3 WHERE id = $1", id, enc, h[:]); err != nil {
			return err
		}
	}
	plain, err := s.Keyring.OpenString(enc, edgecfg.InternalKeyAAD(id))
	if err != nil {
		return err
	}
	sid, err := s.Shadow.CreateKey(ctx, it, groupID, fmt.Sprintf("tm-ik-%d", id), plain)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(ctx, "UPDATE internal_keys SET sub2api_key_id = $2, status = 'active' WHERE id = $1", id, sid)
	return err
}

// Publish 生成 Key 的路由配置并写入 Redis。
func (s *KeyService) Publish(ctx context.Context, keyID int64) error {
	var (
		userID       int64
		keyHash      []byte
		status       string
		reason       *string
		limitsRaw    []byte
		blockedUntil *time.Time
		blockReason  *string
		version      int64
	)
	err := s.DB.QueryRow(ctx, `SELECT user_id, key_hash, status, disabled_reason, limits, limit_blocked_until, limit_block_reason, config_version
		FROM buyer_keys WHERE id = $1`, keyID).Scan(&userID, &keyHash, &status, &reason, &limitsRaw, &blockedUntil, &blockReason, &version)
	if err != nil {
		return err
	}
	hashHex := fmt.Sprintf("%x", keyHash)
	if status == "deleted" {
		return s.Edge.Delete(ctx, hashHex)
	}
	var limits Limits
	if err := json.Unmarshal(limitsRaw, &limits); err != nil {
		return err
	}
	cfg := edgecfg.KeyConfig{
		BuyerKeyID: keyID, BuyerUserID: userID, Status: "active", ExpiresAt: limits.ExpiresAt,
		BlockedUntil: blockedUntil, IPWhitelist: limits.IPWhitelist, Version: version,
	}
	if blockReason != nil {
		cfg.Reason = *blockReason
	}
	if status != "active" {
		cfg.Status = "disabled"
		cfg.Reason = status
		if reason != nil {
			cfg.Reason = *reason
		}
	}
	var locked bool
	if err := s.DB.QueryRow(ctx, "SELECT COALESCE((SELECT keys_locked FROM shadow_identities WHERE kind = 'buyer' AND owner_id = $1), false)", userID).Scan(&locked); err != nil {
		return err
	}
	if locked {
		cfg.Status = "disabled"
		cfg.Reason = "refund in progress"
	}
	rows, err := s.DB.Query(ctx, `SELECT h.position, h.listing_id, h.pool_id, p.name, ik.id, ik.key_enc
		FROM buyer_key_hops h
		LEFT JOIN pools p ON p.id = h.pool_id
		LEFT JOIN listings l ON l.id = h.listing_id
		JOIN internal_keys ik ON ik.buyer_key_id = h.key_id AND ik.status = 'active'
			AND ik.sub2api_group_id = COALESCE(p.sub2api_group_id, l.listing_group_id)
		WHERE h.key_id = $1 ORDER BY h.position`, keyID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var pos int
		var lid, pid *int64
		var pname *string
		var hop edgecfg.Hop
		if err := rows.Scan(&pos, &lid, &pid, &pname, &hop.InternalKeyID, &hop.InternalKeyEnc); err != nil {
			return err
		}
		if lid != nil {
			hop.ListingID = *lid
			hop.Label = fmt.Sprintf("listing:%d", *lid)
		} else {
			hop.PoolID = *pid
			hop.Label = "pool:" + *pname
		}
		cfg.Hops = append(cfg.Hops, hop)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(cfg.Hops) == 0 && cfg.Status == "active" {
		cfg.Status = "disabled"
		cfg.Reason = "provisioning"
	}
	return s.Edge.Put(ctx, hashHex, cfg)
}

const keyColumns = `id, name, key_prefix, route_type, status, disabled_reason, limits, limit_blocked_until, limit_block_reason, created_at`

func (s *KeyService) scanKey(ctx context.Context, row pgx.Row) (*BuyerKey, error) {
	var k BuyerKey
	var limits []byte
	if err := row.Scan(&k.ID, &k.Name, &k.KeyPrefix, &k.RouteType, &k.Status, &k.DisabledReason, &limits, &k.LimitBlockedUntil,
		&k.LimitBlockReason, &k.CreatedAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(limits, &k.Limits); err != nil {
		return nil, err
	}
	skip, _ := s.Edge.SkipList(ctx)
	rows, err := s.DB.Query(ctx, `SELECT h.position, h.listing_id, p.name,
			EXISTS (SELECT 1 FROM internal_keys ik WHERE ik.buyer_key_id = h.key_id AND ik.status = 'active'
				AND ik.sub2api_group_id = COALESCE(p.sub2api_group_id, l.listing_group_id))
		FROM buyer_key_hops h LEFT JOIN pools p ON p.id = h.pool_id LEFT JOIN listings l ON l.id = h.listing_id
		WHERE h.key_id = $1 ORDER BY h.position`, k.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var hv HopView
		var lid *int64
		var pname *string
		var ready bool
		if err := rows.Scan(&hv.Position, &lid, &pname, &ready); err != nil {
			return nil, err
		}
		if lid != nil {
			hv.ListingID = *lid
		}
		if pname != nil {
			hv.Pool = *pname
		}
		switch {
		case !ready:
			hv.State = "provisioning"
		case lid != nil && skip[*lid]:
			hv.State = "skipped"
		default:
			hv.State = "ready"
		}
		k.Hops = append(k.Hops, hv)
	}
	return &k, rows.Err()
}

var ErrKeyNotFound = httpx.NotFound("KEY_NOT_FOUND", "key not found")

func (s *KeyService) Get(ctx context.Context, userID, keyID int64) (*BuyerKey, error) {
	k, err := s.scanKey(ctx, s.DB.QueryRow(ctx, "SELECT "+keyColumns+" FROM buyer_keys WHERE id = $1 AND user_id = $2 AND status <> 'deleted'", keyID, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrKeyNotFound
	}
	return k, err
}

func (s *KeyService) List(ctx context.Context, userID int64) ([]BuyerKey, error) {
	rows, err := s.DB.Query(ctx, "SELECT id FROM buyer_keys WHERE user_id = $1 AND status <> 'deleted' ORDER BY id DESC", userID)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, err
	}
	out := make([]BuyerKey, 0, len(ids))
	for _, id := range ids {
		k, err := s.Get(ctx, userID, id)
		if err != nil {
			return nil, err
		}
		out = append(out, *k)
	}
	return out, nil
}

type UpdateKeyInput struct {
	Name   *string     `json:"name"`
	Route  *RouteInput `json:"route"`
	Limits *Limits     `json:"limits"`
}

// Update 修改名称、路由或限制；路由变化后重新开通并发布。
func (s *KeyService) Update(ctx context.Context, userID, keyID int64, in UpdateKeyInput) (*BuyerKey, error) {
	if _, err := s.Get(ctx, userID, keyID); err != nil {
		return nil, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" || len(name) > 100 {
			return nil, httpx.BadRequest("INVALID_NAME", "name is required (max 100 chars)")
		}
		if _, err := tx.Exec(ctx, "UPDATE buyer_keys SET name = $2 WHERE id = $1", keyID, name); err != nil {
			return nil, err
		}
	}
	if in.Limits != nil {
		if err := in.Limits.validate(); err != nil {
			return nil, err
		}
		raw, _ := json.Marshal(in.Limits)
		if _, err := tx.Exec(ctx, "UPDATE buyer_keys SET limits = $2, limit_blocked_until = NULL, limit_block_reason = NULL WHERE id = $1", keyID, raw); err != nil {
			return nil, err
		}
	}
	if in.Route != nil {
		routeType, hops, err := s.validateRoute(ctx, *in.Route)
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, "UPDATE buyer_keys SET route_type = $2 WHERE id = $1", keyID, routeType); err != nil {
			return nil, err
		}
		if err := insertHops(ctx, tx, keyID, hops); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(ctx, "UPDATE buyer_keys SET config_version = config_version + 1, updated_at = now() WHERE id = $1", keyID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	if err := s.Provision(ctx, keyID); err != nil {
		s.Log.Warn("provision buyer key after update", "key_id", keyID, "err", err)
	}
	return s.Get(ctx, userID, keyID)
}

// SetEnabled 启用或停用 Key。
func (s *KeyService) SetEnabled(ctx context.Context, userID, keyID int64, enabled bool) (*BuyerKey, error) {
	k, err := s.Get(ctx, userID, keyID)
	if err != nil {
		return nil, err
	}
	switch {
	case enabled && k.Status == "disabled":
		_, err = s.DB.Exec(ctx, "UPDATE buyer_keys SET status = 'active', disabled_reason = NULL, config_version = config_version + 1, updated_at = now() WHERE id = $1", keyID)
	case !enabled && k.Status == "active":
		_, err = s.DB.Exec(ctx, "UPDATE buyer_keys SET status = 'disabled', disabled_reason = 'disabled by user', config_version = config_version + 1, updated_at = now() WHERE id = $1", keyID)
	}
	if err != nil {
		return nil, err
	}
	if err := s.Publish(ctx, keyID); err != nil {
		return nil, err
	}
	return s.Get(ctx, userID, keyID)
}

// Delete 删除 Key：立即从 Edge 移除，sub2api 中的内部 key 由清理任务删除。
func (s *KeyService) Delete(ctx context.Context, userID, keyID int64) error {
	if _, err := s.Get(ctx, userID, keyID); err != nil {
		return err
	}
	if _, err := s.DB.Exec(ctx, "UPDATE buyer_keys SET status = 'deleted', deleted_at = now(), updated_at = now() WHERE id = $1", keyID); err != nil {
		return err
	}
	return s.Publish(ctx, keyID)
}

// CleanupDeletedKeys 删除已删除买家 Key 遗留在 sub2api 中的内部 key。
func (s *KeyService) CleanupDeletedKeys(ctx context.Context) (int, error) {
	rows, err := s.DB.Query(ctx, `SELECT ik.id, ik.sub2api_key_id, ik.buyer_user_id FROM internal_keys ik
		JOIN buyer_keys bk ON bk.id = ik.buyer_key_id
		WHERE ik.status <> 'deleted' AND (bk.status = 'deleted'
			OR NOT EXISTS (SELECT 1 FROM buyer_key_hops h LEFT JOIN pools p ON p.id = h.pool_id LEFT JOIN listings l ON l.id = h.listing_id
				WHERE h.key_id = bk.id AND COALESCE(p.sub2api_group_id, l.listing_group_id) = ik.sub2api_group_id))
		LIMIT 100`)
	if err != nil {
		return 0, err
	}
	type item struct {
		id     int64
		subKey *int64
		userID int64
	}
	var items []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.id, &it.subKey, &it.userID); err != nil {
			rows.Close()
			return 0, err
		}
		items = append(items, it)
	}
	rows.Close()
	done := 0
	for _, it := range items {
		if it.subKey != nil {
			ident, err := s.Shadow.Get(ctx, KindBuyer, it.userID)
			if err != nil || ident == nil {
				continue
			}
			if err := s.Shadow.DeleteKey(ctx, ident, *it.subKey); err != nil {
				s.Log.Warn("delete internal key", "internal_key_id", it.id, "err", err)
				continue
			}
		}
		if _, err := s.DB.Exec(ctx, "UPDATE internal_keys SET status = 'deleted' WHERE id = $1", it.id); err != nil {
			return done, err
		}
		done++
	}
	return done, nil
}

// ProvisionPending 重试开通失败的 Key。
func (s *KeyService) ProvisionPending(ctx context.Context) error {
	rows, err := s.DB.Query(ctx, `SELECT id FROM buyer_keys WHERE status = 'provisioning' ORDER BY id LIMIT 50`)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := s.Provision(ctx, id); err != nil {
			s.Log.Warn("retry provision buyer key", "key_id", id, "err", err)
		}
	}
	return nil
}

// PublishAll 全量重新发布所有 Key 的配置，修正 Redis 中的漂移。
func (s *KeyService) PublishAll(ctx context.Context) error {
	rows, err := s.DB.Query(ctx, `SELECT id FROM buyer_keys WHERE status <> 'deleted' OR deleted_at > now() - interval '1 day'`)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := s.Publish(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

// PublishUserKeys 重新发布某买家的全部 Key（例如退款锁定状态变化时）。
func (s *KeyService) PublishUserKeys(ctx context.Context, userID int64) error {
	rows, err := s.DB.Query(ctx, `SELECT id FROM buyer_keys WHERE user_id = $1 AND status <> 'deleted'`, userID)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := s.Publish(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

// PublishSkipList 计算不应接流量的挂单并推送给 Edge：挂单未生效、上游被冻结或删除、健康状态为降智或不可用，
// 或调度器判定不面向买家（例如开启检测的厂商下尚未检测的上游）。
func (s *KeyService) PublishSkipList(ctx context.Context) error {
	rows, err := s.DB.Query(ctx, `SELECT l.id FROM listings l JOIN upstreams u ON u.id = l.upstream_id
		WHERE l.mode = 'consignment' AND (l.status <> 'active' OR u.status <> 'listed' OR u.health IN ('degraded', 'unavailable')
			OR EXISTS (SELECT 1 FROM upstream_runtime_params rp WHERE rp.upstream_id = u.id AND NOT rp.buyer_facing))`)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return err
	}
	return s.Edge.SetSkipList(ctx, ids)
}

// SetBuyerLocked 退款期间锁定或解锁买家的全部 Key。
func (s *KeyService) SetBuyerLocked(ctx context.Context, userID int64, locked bool) error {
	if _, err := s.DB.Exec(ctx, "UPDATE shadow_identities SET keys_locked = $2, updated_at = now() WHERE kind = 'buyer' AND owner_id = $1", userID, locked); err != nil {
		return err
	}
	return s.PublishUserKeys(ctx, userID)
}
