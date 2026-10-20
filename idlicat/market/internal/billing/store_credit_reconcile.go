package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// 隔离活动钱包单独对账；优惠面额永远不计入普通买家可退款现金。
func (r *Reconciler) runStoreCreditBalances(ctx context.Context, tolerance float64) (int64, error) {
	rows, err := r.DB.Query(ctx, `SELECT p.id,p.buyer_id,si.sub2api_user_id,
 CASE WHEN p.refund_op_id IS NOT NULL AND (SELECT status FROM sub2api_ops WHERE id=p.refund_op_id)='done'
 THEN 0 ELSE p.remaining_balance_units END::float8
 FROM store_credit_packs p JOIN shadow_identities si ON si.kind='credit_pack' AND si.owner_id=p.id
 WHERE si.sub2api_user_id IS NOT NULL AND p.funding_op_id IS NOT NULL`)
	if err != nil {
		return 0, err
	}
	type item struct {
		id, buyer, sub int64
		expected       float64
	}
	items := []item{}
	ids := []int64{}
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.id, &it.buyer, &it.sub, &it.expected); err != nil {
			rows.Close()
			return 0, err
		}
		items = append(items, it)
		ids = append(ids, it.sub)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(items) == 0 {
		return 0, nil
	}
	bs, err := r.Replica.UserBalances(ctx, ids)
	if err != nil {
		return 0, err
	}
	actual := map[int64]float64{}
	for _, b := range bs {
		actual[b.ID] = b.Balance
	}
	var cursor int64
	if err := r.DB.QueryRow(ctx, "SELECT last_id FROM ingest_cursors WHERE source='usage_logs'").Scan(&cursor); err != nil {
		return 0, err
	}
	pending, err := r.Replica.UsageSumAfter(ctx, cursor, ids)
	if err != nil {
		return 0, err
	}
	var found int64
	for _, it := range items {
		var inflight float64
		if err := r.DB.QueryRow(ctx, `SELECT COALESCE(sum(CASE WHEN op_type='user.balance_add' THEN (payload->>'amount')::numeric
 ELSE -(payload->>'amount')::numeric END),0)::float8 FROM sub2api_ops
 WHERE target=$1 AND status IN('pending','sending','failed') AND op_type IN('user.balance_add','user.balance_subtract')
 AND idempotency_key NOT LIKE 'tm-credit-refund-debit-%'`, fmt.Sprintf("user:%d", it.sub)).Scan(&inflight); err != nil {
			return found, err
		}
		diff := actual[it.sub] + inflight - (it.expected - pending[it.sub])
		if abs(diff) <= tolerance {
			continue
		}
		detail, _ := json.Marshal(map[string]any{"credit_pack_id": it.id, "sub2api_balance": actual[it.sub], "remaining_balance_units": it.expected, "uningested_usage": pending[it.sub], "inflight_ops": inflight, "diff": diff})
		tag, err := r.DB.Exec(ctx, `INSERT INTO billing_anomalies(rule,dedupe_key,buyer_user_id,amount,detail)
 VALUES('balance_mismatch',$1,$2,$3,$4) ON CONFLICT(dedupe_key) DO NOTHING`, fmt.Sprintf("credit-balance:%d:%s", it.id, r.now().Format("2006-01-02")), it.buyer, diff, detail)
		if err != nil {
			return found, err
		}
		found += tag.RowsAffected()
	}
	return found, nil
}

// retryCreditRemainders 收回最后一笔的现金欠款。买家有欠款期间不能再用活动包；充值后自动重试。
// 不把 dead 扣款当已付，也不重新记账或新增幂等键。
func (f *Funds) retryCreditRemainders(ctx context.Context) error {
	rows, err := f.DB.Query(ctx, `SELECT o.id,(o.payload->>'user_id')::bigint,(o.payload->>'amount')::float8
 FROM sub2api_ops o WHERE o.idempotency_key LIKE 'tm-credit-cash-%' AND o.status='dead' ORDER BY o.id`)
	if err != nil {
		return err
	}
	type item struct {
		id, sub int64
		amount  float64
	}
	items := []item{}
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.id, &it.sub, &it.amount); err != nil {
			rows.Close()
			return err
		}
		items = append(items, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, it := range items {
		bs, err := f.Replica.UserBalances(ctx, []int64{it.sub})
		if err != nil {
			return err
		}
		if len(bs) == 1 && bs[0].Balance >= it.amount {
			if _, err := f.DB.Exec(ctx, "UPDATE sub2api_ops SET status='pending',attempts=0,next_attempt_at=now(),updated_at=now() WHERE id=$1 AND status='dead'", it.id); err != nil {
				return err
			}
		}
	}
	return nil
}

// 活动内部凭证沿用 R3 门槛；先在选择器数据库入口停用，再尽力同步 sub2api。
func (r *Reconciler) freezeOrphanCreditKeys(ctx context.Context, since time.Time) (int, error) {
	rows, err := r.DB.Query(ctx, `SELECT ck.id,ck.pack_id,ck.sub2api_key_id
 FROM billing_anomalies ba JOIN usage_records ur ON ur.sub2api_usage_id=ba.sub2api_usage_id
 JOIN store_credit_keys ck ON ck.sub2api_key_id=ur.sub2api_api_key_id
 WHERE ba.rule='R3_orphan_charge' AND ba.status='open' AND ba.created_at>$1 AND ck.status='active'
 GROUP BY ck.id,ck.pack_id,ck.sub2api_key_id HAVING count(*)>=3`, since)
	if err != nil {
		return 0, err
	}
	type item struct{ id, pack, sub int64 }
	items := []item{}
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.id, &it.pack, &it.sub); err != nil {
			rows.Close()
			return 0, err
		}
		items = append(items, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	done := 0
	for _, it := range items {
		tag, err := r.DB.Exec(ctx, "UPDATE store_credit_keys SET status='frozen' WHERE id=$1 AND status='active'", it.id)
		if err != nil {
			return done, err
		}
		if tag.RowsAffected() == 0 {
			continue
		}
		done++
		ident, err := r.Shadow.Get(ctx, "credit_pack", it.pack)
		if err == nil && ident != nil {
			err = r.Shadow.SetKeyStatus(ctx, ident, it.sub, false)
		}
		if err != nil {
			r.Log.Error("freeze store credit internal key", "key_id", it.id, "pack_id", it.pack, "err", err)
		}
	}
	return done, nil
}
