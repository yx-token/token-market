package buyer

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/yx-token/token-market/market/internal/edgecfg"
	"github.com/yx-token/token-market/market/internal/listing"
	"github.com/yx-token/token-market/market/internal/sub2api/adminclient"
	"github.com/yx-token/token-market/market/internal/sub2api/ops"
	"github.com/yx-token/token-market/market/internal/sub2api/opshandlers"
)

// KindCreditPack 的余额只属于一个店铺活动包，绝不能注入普通 buyer 身份。
const KindCreditPack = "credit_pack"

// ProvisionCreditPacks 由 worker 反复调用。购买扣款完成后才给隔离钱包充值，
// 充值完成后才启用活动包；所有远端操作都可以在进程中断后安全重试。
func (s *KeyService) ProvisionCreditPacks(ctx context.Context) error {
	rows, err := s.DB.Query(ctx, `SELECT p.id FROM store_credit_packs p
		JOIN sub2api_ops purchase ON purchase.id = p.purchase_op_id AND purchase.status = 'done'
		WHERE p.status IN ('provisioning', 'active') ORDER BY p.id`)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return err
	}
	var errs []error
	for _, id := range ids {
		if err := s.provisionCreditPack(ctx, id); err != nil {
			errs = append(errs, fmt.Errorf("credit pack %d: %w", id, err))
		}
		if ctx.Err() != nil {
			break
		}
	}
	return errors.Join(errs...)
}

func (s *KeyService) provisionCreditPack(ctx context.Context, packID int64) error {
	// 与 Edge 请求及退款使用同一把锁。会话锁允许先提交凭证再调远端，
	// 避免远端创建成功、本地事务回滚后丢失可恢复的 custom_key。
	conn, err := s.DB.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	lockName := fmt.Sprintf("store-credit-pack:%d", packID)
	var locked bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1, 0))`, lockName).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return nil // 活跃请求/退款正在持锁；下一轮再开通。
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(cleanup, `SELECT pg_advisory_unlock(hashtextextended($1, 0))`, lockName); err != nil {
			_ = conn.Conn().Close(cleanup) // 不把仍带锁的连接交回池。
		}
	}()

	var buyerID, sellerID int64
	var fundingUnits float64
	var status, purchaseStatus string
	var fundingOpID *int64
	err = conn.QueryRow(ctx, `SELECT p.buyer_id, p.seller_id, p.remaining_balance_units::float8,
		p.status, p.funding_op_id, purchase.status
		FROM store_credit_packs p JOIN sub2api_ops purchase ON purchase.id = p.purchase_op_id
		WHERE p.id = $1`, packID).Scan(&buyerID, &sellerID, &fundingUnits, &status, &fundingOpID, &purchaseStatus)
	if err != nil {
		return err
	}
	if (status != "provisioning" && status != "active") || purchaseStatus != ops.StatusDone {
		return nil
	}
	it, err := s.Shadow.Ensure(ctx, KindCreditPack, packID)
	if err != nil {
		return err
	}
	if it.Sub2APIUserID == nil {
		return errors.New("credit shadow user not ready")
	}
	if fundingOpID == nil {
		// 包内优惠只能走被显式授权的本店专属分组。
		restrict := true
		if _, err := s.Shadow.Admin.UpdateUser(ctx, *it.Sub2APIUserID, adminclient.UpdateUserInput{RestrictPublicGroups: &restrict}); err != nil {
			return err
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
		opID, err := ops.Enqueue(ctx, tx, ops.NewOp{
			Type: opshandlers.TypeUserBalanceAdd, Target: fmt.Sprintf("user:%d", *it.Sub2APIUserID),
			Payload: opshandlers.BalancePayload{UserID: *it.Sub2APIUserID, Amount: fundingUnits,
				Notes: fmt.Sprintf("tm store credit pack %d", packID)},
			IdempotencyKey: fmt.Sprintf("tm-credit-fund-%d", packID),
		})
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE store_credit_packs SET funding_op_id = $2 WHERE id = $1`, packID, opID); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	fundingStatus, err := ops.Status(ctx, conn, *fundingOpID)
	if err != nil {
		return err
	}
	if fundingStatus != ops.StatusDone {
		return nil
	}
	if status == "provisioning" {
		if _, err := conn.Exec(ctx, `UPDATE store_credit_packs SET status = 'active'
			WHERE id = $1 AND status = 'provisioning'`, packID); err != nil {
			return err
		}
	}
	// 从普通买家 Key 的实际路由生成内部活动凭证，不从所有 seller 上游遍历，
	// 也不接管旧 quota_packs 的专用 Key。
	rows, err := conn.Query(ctx, `SELECT DISTINCT k.id, l.id, l.listing_group_id
		FROM buyer_keys k JOIN buyer_key_hops h ON h.key_id = k.id
		JOIN listings l ON l.id = h.listing_id
		JOIN upstreams u ON u.id = l.upstream_id AND u.seller_id = l.seller_id
		JOIN buyer_merchants bm ON bm.user_id = k.user_id AND bm.seller_id = l.seller_id
		JOIN internal_keys ik ON ik.buyer_key_id = k.id
			AND ik.sub2api_group_id = l.listing_group_id AND ik.status = 'active'
		WHERE k.user_id = $1 AND k.status = 'active' AND l.seller_id = $2
			AND `+listing.MarketAvailableSQL+`
			AND l.listing_group_id IS NOT NULL
			AND NOT EXISTS (SELECT 1 FROM quota_packs q WHERE q.buyer_key_id = k.id)
		ORDER BY k.id, l.id`, buyerID, sellerID)
	if err != nil {
		return err
	}
	type creditHop struct{ keyID, listingID, groupID int64 }
	hops, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (creditHop, error) {
		var h creditHop
		err := row.Scan(&h.keyID, &h.listingID, &h.groupID)
		return h, err
	})
	if err != nil {
		return err
	}
	publish := map[int64]bool{}
	for _, h := range hops {
		if err := s.Shadow.AllowGroup(ctx, it.ID, h.groupID); err != nil {
			return err
		}
		if err := s.ensureCreditInternalKey(ctx, it, packID, h.keyID, h.listingID, h.groupID); err != nil {
			return err
		}
		publish[h.keyID] = true
	}
	for keyID := range publish {
		if err := s.Publish(ctx, keyID); err != nil {
			return err
		}
	}
	return nil
}

func (s *KeyService) ensureCreditInternalKey(ctx context.Context, it *Identity, packID, keyID, listingID, groupID int64) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var id int64
	var subKeyID *int64
	var enc []byte
	var status string
	err = tx.QueryRow(ctx, `SELECT id, sub2api_key_id, key_enc, status FROM store_credit_keys
		WHERE pack_id = $1 AND buyer_key_id = $2 AND listing_id = $3 FOR UPDATE`, packID, keyID, listingID).
		Scan(&id, &subKeyID, &enc, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		plain := "tmck_" + randomString(40)
		hash := sha256.Sum256([]byte(plain))
		if err := tx.QueryRow(ctx, `INSERT INTO store_credit_keys
			(pack_id, buyer_key_id, listing_id, sub2api_group_id, key_enc, key_hash)
			VALUES ($1,$2,$3,$4,'\x00',$5) RETURNING id`, packID, keyID, listingID, groupID, hash[:]).Scan(&id); err != nil {
			return err
		}
		enc, err = s.Keyring.SealString(plain, edgecfg.CreditKeyAAD(id))
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE store_credit_keys SET key_enc = $2 WHERE id = $1`, id, enc); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if status == "frozen" || status == "deleted" {
		return nil
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if subKeyID != nil {
		return nil
	}
	plain, err := s.Keyring.OpenString(enc, edgecfg.CreditKeyAAD(id))
	if err != nil {
		return err
	}
	subID, err := s.Shadow.CreateKey(ctx, it, groupID, fmt.Sprintf("tm-credit-key-%d", id), plain)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(ctx, `UPDATE store_credit_keys SET sub2api_key_id = $2, status = 'active'
		WHERE id = $1 AND status = 'provisioning'`, id, subID)
	return err
}
