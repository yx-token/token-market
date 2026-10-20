package listing

import "context"

// MarketAvailableSQL 用于 l=listings、u=upstreams 的查询，统一市场和显式买家路由的安全边界。
const MarketAvailableSQL = `l.mode IN ('consignment', 'pack') AND l.status = 'active'
 AND u.status = 'listed' AND NOT u.is_reference AND u.sub2api_account_id IS NOT NULL
 AND u.health IN ('unknown', 'healthy', 'limited', 'suspected')
 AND (u.auth_type <> 'oauth' OR u.platform NOT IN ('openai', 'antigravity') OR u.privacy_ok IS TRUE)
 AND EXISTS (SELECT 1 FROM vendor_configs v WHERE v.platform = u.platform AND v.enabled)
 AND NOT EXISTS (SELECT 1 FROM upstream_runtime_params rp WHERE rp.upstream_id = u.id AND NOT rp.buyer_facing)`

func MarketAvailable(ctx context.Context, db DB, listingID int64) (bool, error) {
	var ok bool
	err := db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM listings l JOIN upstreams u ON u.id = l.upstream_id
		WHERE l.id = $1 AND `+MarketAvailableSQL+`)`, listingID).Scan(&ok)
	return ok, err
}
