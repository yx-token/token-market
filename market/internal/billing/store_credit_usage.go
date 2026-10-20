package billing

import (
	"context"
	"fmt"

	"github.com/yx-token/idlicat/market/internal/sub2api/ops"
	"github.com/yx-token/idlicat/market/internal/sub2api/opshandlers"
	"github.com/jackc/pgx/v5"
)

// allocateStoreCredits 在与 usage 认领和复式账本同一事务内分摊，精度均在 SQL numeric 中计算。
// credit_pack_id 来自不对外暴露的隔离钱包 key；普通现金/旧额度包不经过本函数。
func allocateStoreCredits(ctx context.Context, tx pgx.Tx, txnID int64) error {
	rows, err := tx.Query(ctx, `SELECT sub2api_usage_id,credit_pack_id FROM usage_records
 WHERE ledger_txn_id=$1 AND credit_pack_id IS NOT NULL ORDER BY created_at,sub2api_usage_id`, txnID)
	if err != nil {
		return err
	}
	type item struct{ usageID, packID int64 }
	items := []item{}
	for rows.Next() {
		var i item
		if err := rows.Scan(&i.usageID, &i.packID); err != nil {
			rows.Close()
			return err
		}
		items = append(items, i)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, it := range items {
		// 无论状态是否已耗尽，都由行锁保证同包剩余额度最多释放一次。refunding 期间处理冻结前已经发生的调用。
		var packID int64
		if err := tx.QueryRow(ctx, "SELECT id FROM store_credit_packs WHERE id=$1 FOR UPDATE", it.packID).Scan(&packID); err != nil {
			return err
		}
		var buyerID int64
		var coveredUSD, leftUSD, covered, prepaid, cash, payout string
		err := tx.QueryRow(ctx, `WITH x AS (
 SELECT p.buyer_id,p.remaining_balance_units,p.usd_to_balance_rate,p.prepaid_remaining_cny,p.price_cny,p.face_value_usd,
 ur.actual_cost,ur.payout, GREATEST(0,LEAST(ur.actual_cost,CASE WHEN
 p.status IN ('active','refunding') AND p.seller_id=ur.seller_id
 AND (cardinality(p.allowed_platforms)=0 OR u.platform=ANY(p.allowed_platforms))
 AND (cardinality(p.allowed_models)=0 OR ur.model=ANY(p.allowed_models))
 THEN p.remaining_balance_units ELSE 0 END)) AS covered
 FROM store_credit_packs p JOIN usage_records ur ON ur.sub2api_usage_id=$2
 JOIN upstreams u ON u.id=ur.upstream_id WHERE p.id=$1
 ), split AS (
 SELECT *,CASE WHEN covered=remaining_balance_units AND covered>0 THEN prepaid_remaining_cny
 ELSE LEAST(prepaid_remaining_cny,round(covered*price_cny/(face_value_usd*usd_to_balance_rate),10)) END AS prepaid
 FROM x)
 SELECT buyer_id,round(covered/usd_to_balance_rate,16)::text,
 round((remaining_balance_units-covered)/usd_to_balance_rate,16)::text,
 covered::text,prepaid::text,GREATEST(actual_cost-covered,0)::text,
 CASE WHEN actual_cost>0 THEN round(payout*(GREATEST(actual_cost-covered,0)+prepaid)/actual_cost,10) ELSE payout END::text
 FROM split`, it.packID, it.usageID).Scan(&buyerID, &coveredUSD, &leftUSD, &covered, &prepaid, &cash, &payout)
		if err != nil {
			return fmt.Errorf("allocate credit usage %d: %w", it.usageID, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO store_credit_allocations(usage_id,pack_id,buyer_id,seller_id,model,platform,covered_usd,covered_balance_units,prepaid_cny,cash_cny)
 SELECT ur.sub2api_usage_id,$1,$3,ur.seller_id,ur.model,u.platform,$4::numeric,$5::numeric,$6::numeric,$7::numeric
 FROM usage_records ur JOIN upstreams u ON u.id=ur.upstream_id WHERE ur.sub2api_usage_id=$2`, it.packID, it.usageID, buyerID, coveredUSD, covered, prepaid, cash); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE store_credit_packs SET remaining_usd=$2::numeric,
 remaining_balance_units=remaining_balance_units-$3::numeric,prepaid_remaining_cny=prepaid_remaining_cny-$4::numeric,
 status=CASE WHEN remaining_balance_units=$3::numeric AND status='active' THEN 'depleted' ELSE status END WHERE id=$1`, it.packID, leftUSD, covered, prepaid); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE usage_records SET credit_covered_units=$2::numeric,credit_prepaid_cny=$3::numeric,
 cash_cost=$4::numeric,payout=$5::numeric WHERE sub2api_usage_id=$1`, it.usageID, covered, prepaid, cash, payout); err != nil {
			return err
		}
		// 隔离钱包已扣全部请求费用；把超过包余额/不匹配的部分转给普通钱包。两侧 outbox 都幂等，
		// 对隔离钱包的加回仅恢复误占的部分，使其最终余额恰好等于包余量，绝不是给全平台充值优惠。
		var cashF float64
		var buyerSubID, packSubID *int64
		err = tx.QueryRow(ctx, `SELECT $1::numeric::float8,
 (SELECT sub2api_user_id FROM shadow_identities WHERE kind='buyer' AND owner_id=$2),
 (SELECT sub2api_user_id FROM shadow_identities WHERE kind='credit_pack' AND owner_id=$3)`, cash, buyerID, it.packID).Scan(&cashF, &buyerSubID, &packSubID)
		if err != nil {
			return err
		}
		if cashF > 0 {
			if buyerSubID == nil || packSubID == nil {
				return fmt.Errorf("credit usage %d lacks shadow wallet", it.usageID)
			}
			for _, op := range []ops.NewOp{
				{Type: opshandlers.TypeUserBalanceSubtract, Target: fmt.Sprintf("user:%d", *buyerSubID), Payload: opshandlers.BalancePayload{UserID: *buyerSubID, Amount: cashF, Notes: fmt.Sprintf("tm credit remainder usage %d", it.usageID)}, IdempotencyKey: fmt.Sprintf("tm-credit-cash-%d", it.usageID), Priority: ops.PriorityUrgent},
				{Type: opshandlers.TypeUserBalanceAdd, Target: fmt.Sprintf("user:%d", *packSubID), Payload: opshandlers.BalancePayload{UserID: *packSubID, Amount: cashF, Notes: fmt.Sprintf("tm credit remainder restore %d", it.usageID)}, IdempotencyKey: fmt.Sprintf("tm-credit-restore-%d", it.usageID), Priority: ops.PriorityUrgent},
			} {
				if _, err := ops.Enqueue(ctx, tx, op); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
