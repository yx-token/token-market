package edge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/yx-token/idlicat/market/internal/edgecfg"
	"github.com/yx-token/idlicat/market/internal/listing"
	"github.com/yx-token/idlicat/market/internal/platform/crypto"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CreditLease 的锁覆盖完整上游响应（包括 SSE），与退款/同包请求互斥。
type CreditLease struct {
	PackID  int64
	Key     string
	Release func()
}

type CreditSelector interface {
	Select(context.Context, *edgecfg.KeyConfig, edgecfg.Hop, string, int64) (*CreditLease, error)
}

type prefixReadCloser struct {
	io.Reader
	io.Closer
}

// SetCreditSelector 在 HTTP 服务启动前注入；nil 保留原有的普通余额路径。
func (s *Server) SetCreditSelector(selector CreditSelector) { s.credits = selector }

type DBCreditSelector struct {
	DB      *pgxpool.Pool
	Keyring *crypto.Keyring
}

// 除商品作用域之外再验证原 Key、当前路由与普通内部凭证，Redis 缓存中的
// 旧路由不能拿别家活动额度，也不能绕过普通 Key 的退款锁/停用/花费限额。
const eligibleCredit = `
	FROM store_credit_keys ck
	JOIN store_credit_packs p ON p.id = ck.pack_id
	JOIN buyer_keys bk ON bk.id = ck.buyer_key_id AND bk.user_id = p.buyer_id
	JOIN buyer_key_hops bh ON bh.key_id = bk.id AND bh.listing_id = ck.listing_id
	JOIN listings l ON l.id = ck.listing_id AND l.seller_id = p.seller_id
	JOIN upstreams u ON u.id = l.upstream_id AND u.seller_id = p.seller_id
	JOIN buyer_merchants bm ON bm.user_id = p.buyer_id AND bm.seller_id = p.seller_id
	JOIN internal_keys ik ON ik.id = $5 AND ik.buyer_key_id = bk.id
		AND ik.sub2api_group_id = l.listing_group_id AND ik.status = 'active'
	JOIN shadow_identities buyer ON buyer.kind = 'buyer' AND buyer.owner_id = p.buyer_id
		AND buyer.status = 'active' AND NOT buyer.keys_locked
	JOIN shadow_identities credit ON credit.kind = 'credit_pack' AND credit.owner_id = p.id
		AND credit.status = 'active' AND NOT credit.keys_locked
	JOIN sub2api_ops funding ON funding.id = p.funding_op_id AND funding.status = 'done'
	WHERE p.buyer_id = $1 AND ck.buyer_key_id = $2 AND ck.listing_id = $3
		AND p.status = 'active' AND p.remaining_balance_units > 0
		AND ck.status = 'active' AND ck.sub2api_key_id IS NOT NULL
		AND ck.sub2api_group_id = l.listing_group_id
		AND ` + listing.MarketAvailableSQL + `
		AND (cardinality(u.model_whitelist) = 0 OR ($4 <> '' AND $4 = ANY(u.model_whitelist)))
		AND bk.status = 'active' AND (bk.limit_blocked_until IS NULL OR bk.limit_blocked_until <= now())
		AND (bk.limits->>'expires_at' IS NULL OR (bk.limits->>'expires_at')::timestamptz > now())
		AND NOT EXISTS (SELECT 1 FROM quota_packs q WHERE q.buyer_key_id = bk.id)
		AND NOT EXISTS (SELECT 1 FROM sub2api_ops debt WHERE debt.target = 'user:' || buyer.sub2api_user_id
			AND debt.idempotency_key LIKE 'tm-credit-cash-%' AND debt.status <> 'done')
		AND (cardinality(p.allowed_platforms) = 0 OR u.platform = ANY(p.allowed_platforms))
		AND (cardinality(p.allowed_models) = 0 OR ($4 <> '' AND $4 = ANY(p.allowed_models)))`

// afterPackID 只在前一个包被 sub2api 明确以余额不足拒绝后前移，避免未归集的
// 本地余额让已耗尽的首包挡住后续仍有额度的活动包。
func (s *DBCreditSelector) Select(ctx context.Context, cfg *edgecfg.KeyConfig, hop edgecfg.Hop, model string, afterPackID int64) (*CreditLease, error) {
	if cfg.BuyerUserID <= 0 || cfg.BuyerKeyID <= 0 || hop.ListingID <= 0 || hop.KeyAAD != "" {
		return nil, nil
	}
	if s.DB == nil || s.Keyring == nil {
		return nil, errors.New("credit database unavailable")
	}
	args := []any{cfg.BuyerUserID, cfg.BuyerKeyID, hop.ListingID, model, hop.InternalKeyID, afterPackID}
	rows, err := s.DB.Query(ctx, `SELECT DISTINCT p.id `+eligibleCredit+` AND p.id > $6 ORDER BY p.id`, args...)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		tx, err := s.DB.Begin(ctx)
		if err != nil {
			return nil, err
		}
		release := creditRelease(tx)
		// 只拿 advisory 锁，不锁 pack 行，允许后台为刚完成的 usage 入账。
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, fmt.Sprintf("store-credit-pack:%d", id)); err != nil {
			release()
			return nil, err
		}
		var keyID int64
		var enc []byte
		err = tx.QueryRow(ctx, `SELECT ck.id, ck.key_enc `+eligibleCredit+` AND p.id > $6 AND p.id = $7`, append(args, id)...).Scan(&keyID, &enc)
		if errors.Is(err, pgx.ErrNoRows) {
			release()
			continue // 等锁时可能已耗尽/退款；重读后选下一个包。
		}
		if err != nil {
			release()
			return nil, err
		}
		key, err := s.Keyring.OpenString(enc, edgecfg.CreditKeyAAD(keyID))
		if err != nil {
			release()
			return nil, err
		}
		return &CreditLease{PackID: id, Key: key, Release: release}, nil
	}
	return nil, nil
}

func creditRelease(tx pgx.Tx) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = tx.Rollback(ctx)
		})
	}
}

// Gemini 的模型来自 URL；其余协议来自 body.model。无法确定时返回空串，
// 受模型限制的包通过 SQL fail closed，保持普通平台余额路径。
func creditRequestModel(path string, body []byte) string {
	if _, rest, ok := strings.Cut(path, "/models/"); ok {
		if model, action, ok := strings.Cut(rest, ":"); ok && model != "" && !strings.Contains(model, "/") {
			switch action {
			case "generateContent", "streamGenerateContent", "embedContent", "batchEmbedContents", "predict", "predictLongRunning":
				return model
			}
		}
		return ""
	}
	var payload struct {
		Model string `json:"model"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return ""
	}
	return strings.TrimSpace(payload.Model)
}

// 只识别 sub2api 准入错误，不能因为卖家正文提到余额就重放已计费请求。
func creditInsufficient(status int, body []byte) bool {
	if status != http.StatusForbidden {
		return false
	}
	var payload struct {
		Code  string `json:"code"`
		Error struct {
			Code string `json:"code"`
			Type string `json:"type"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return false
	}
	return payload.Code == "INSUFFICIENT_BALANCE" || payload.Error.Code == "INSUFFICIENT_BALANCE" || payload.Error.Type == "INSUFFICIENT_BALANCE"
}
