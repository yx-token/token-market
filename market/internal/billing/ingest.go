package billing

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/yx-token/token-market/market/internal/sub2api/replica"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// ingestSafetyLag：只读取早于当前时间这么久的 usage_logs，因为自增 id 的提交顺序不一定与 id 顺序一致
	ingestSafetyLag = 2 * time.Minute
	// ingestOverlap：每次从游标往前回退的 id 数量，配合主键去重补上晚提交的记录
	ingestOverlap = 2000
	ingestBatch   = 2000
)

type Ingester struct {
	DB      *pgxpool.Pool
	Replica *replica.Reader
	Log     *slog.Logger
	Now     func() time.Time
	// Gate 为 false 时暂停入账（sub2api 兼容自检未通过时，用量的含义可能已经变化）
	Gate func(ctx context.Context) (bool, string)
}

func (in *Ingester) now() time.Time {
	if in.Now != nil {
		return in.Now()
	}
	return time.Now()
}

// IngestResult 汇总一轮入账。
type IngestResult struct {
	Fetched  int
	Inserted int
	Posted   int64
}

// RunOnce 拉取一批用量、解析归属、记账、汇总结算行。返回是否可能还有更多数据。
func (in *Ingester) RunOnce(ctx context.Context) (*IngestResult, bool, error) {
	var cursor int64
	if err := in.DB.QueryRow(ctx, "SELECT last_id FROM ingest_cursors WHERE source = 'usage_logs'").Scan(&cursor); err != nil {
		return nil, false, err
	}
	from := cursor - ingestOverlap
	if from < 0 {
		from = 0
	}
	logs, err := in.Replica.UsageLogsAfter(ctx, from, in.now().Add(-ingestSafetyLag), ingestOverlap+ingestBatch)
	if err != nil {
		return nil, false, err
	}
	res := &IngestResult{Fetched: len(logs)}
	if len(logs) == 0 {
		return res, false, nil
	}

	tx, err := in.DB.Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// 防止多个入账器用旧游标覆盖新游标；锁覆盖导入、归属与资金入账。
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(1953325673)"); err != nil {
		return nil, false, err
	}

	maxID := cursor
	for _, u := range logs {
		if u.ID > maxID {
			maxID = u.ID
		}
		tag, err := tx.Exec(ctx, `INSERT INTO usage_records (sub2api_usage_id, sub2api_request_id, created_at, sub2api_account_id, sub2api_group_id,
				sub2api_user_id, sub2api_api_key_id, model, input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens,
				total_cost, actual_cost, account_rate_multiplier, first_token_ms, duration_ms, upstream_response_model, upstream_model_mismatch, channel)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, 'unknown')
			ON CONFLICT (sub2api_usage_id) DO NOTHING`,
			u.ID, u.RequestID, u.CreatedAt, u.AccountID, u.GroupID, u.UserID, u.APIKeyID, u.Model, u.InputTokens, u.OutputTokens,
			u.CacheCreationTokens, u.CacheReadTokens, u.TotalCost, u.ActualCost, u.AccountRateMultiplier, u.FirstTokenMs,
			u.DurationMs, u.UpstreamResponseModel, u.UpstreamModelMismatch)
		if err != nil {
			return nil, false, fmt.Errorf("insert usage %d: %w", u.ID, err)
		}
		res.Inserted += int(tag.RowsAffected())
	}
	if err := attribute(ctx, tx, from); err != nil {
		return nil, false, err
	}
	posted, err := postUsage(ctx, tx, from)
	if err != nil {
		return nil, false, err
	}
	res.Posted = posted
	if _, err := tx.Exec(ctx, "UPDATE ingest_cursors SET last_id = GREATEST(last_id, $1), updated_at = now() WHERE source = 'usage_logs'", maxID); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, err
	}
	return res, len(logs) == ingestOverlap+ingestBatch, nil
}

// attribute 为新入库（channel = unknown 且尚未记账）的用量解析业务归属。
func attribute(ctx context.Context, tx pgx.Tx, fromID int64) error {
	// 上游、卖家与挂单
	if _, err := tx.Exec(ctx, `UPDATE usage_records ur SET upstream_id = u.id, seller_id = u.seller_id,
			payout = ur.total_cost * COALESCE(ur.account_rate_multiplier, 1),
			listing_id = (SELECT l.id FROM listings l WHERE l.upstream_id = u.id AND l.created_at <= ur.created_at
				AND (l.closed_at IS NULL OR l.closed_at > ur.created_at) ORDER BY l.id DESC LIMIT 1)
		FROM upstreams u
		WHERE u.sub2api_account_id = ur.sub2api_account_id AND ur.upstream_id IS NULL AND ur.ledger_txn_id IS NULL
			AND ur.sub2api_usage_id > $1`, fromID); err != nil {
		return err
	}
	// 买家与渠道：内部 key → 买家 Key；分组决定是默认分组还是指定卖家
	if _, err := tx.Exec(ctx, `UPDATE usage_records ur SET buyer_key_id = ik.buyer_key_id, buyer_user_id = ik.buyer_user_id,
			channel = CASE
				WHEN EXISTS (SELECT 1 FROM pools p WHERE p.sub2api_group_id = ur.sub2api_group_id) THEN 'pool'
				WHEN EXISTS (SELECT 1 FROM listings l WHERE l.listing_group_id = ur.sub2api_group_id) THEN 'direct'
				ELSE 'unknown' END
		FROM internal_keys ik
		WHERE ik.sub2api_key_id = ur.sub2api_api_key_id AND ur.channel = 'unknown' AND ur.ledger_txn_id IS NULL
			AND ur.sub2api_usage_id > $1`, fromID); err != nil {
		return err
	}
	// 活动钱包内部 key 仍归属原买家 Key，仅限本店；归属时再次由真实上游/模型校验作用域。
	if _, err := tx.Exec(ctx, `UPDATE usage_records ur SET buyer_key_id=ck.buyer_key_id,
		buyer_user_id=cp.buyer_id, credit_pack_id=cp.id, channel='direct'
		FROM store_credit_keys ck JOIN store_credit_packs cp ON cp.id=ck.pack_id
		WHERE ck.sub2api_key_id=ur.sub2api_api_key_id AND ur.channel='unknown'
		AND ur.ledger_txn_id IS NULL AND ur.sub2api_usage_id>$1`, fromID); err != nil {
		return err
	}
	// 探测流量
	if _, err := tx.Exec(ctx, `UPDATE usage_records ur SET channel = 'probe'
		FROM system_keys sk
		WHERE sk.sub2api_key_id = ur.sub2api_api_key_id AND ur.channel = 'unknown' AND ur.ledger_txn_id IS NULL
			AND ur.sub2api_usage_id > $1`, fromID); err != nil {
		return err
	}
	// 上架检测（含上架失败后重新检测）与卖家手动复测由卖家自己承担：不结算收益，也不计平台探测成本，
	// 否则卖家可以反复复测、用自己的额度换平台的钱。探测请求不带检测记录号，按同一上游的检测时间窗归属；
	// 第三方检测在检测记录结束前完成，同样落在窗口内。窗口取排队到结束，不留尾差：检测可能紧挨着先后完成，
	// 尾差会把随后平台发起的检测也算到卖家头上。卖家复测排队时不能有其他检测在跑，
	// 只有管理员或申诉检测可能与之重叠，重叠部分同样由卖家承担。
	_, err := tx.Exec(ctx, `UPDATE usage_records ur SET payout = 0, seller_borne = true
		WHERE ur.channel = 'probe' AND ur.upstream_id IS NOT NULL AND ur.ledger_txn_id IS NULL AND ur.sub2api_usage_id > $1
			AND EXISTS (SELECT 1 FROM probe_runs r WHERE r.upstream_id = ur.upstream_id AND r.trigger IN ('onboarding', 'seller_manual')
				AND ur.created_at >= r.queued_at AND ur.created_at <= COALESCE(r.finished_at, 'infinity'::timestamptz))`, fromID)
	return err
}

// postUsage 把尚未记账、已归属的用量记成一笔 usage 交易，并汇总到结算行。
// 买家：买家余额减少 actual_cost；卖家：待结算增加 payout；差额记平台收入；探测流量记平台探测成本（卖家承担的探测 payout 为 0，不入账）。
func postUsage(ctx context.Context, tx pgx.Tx, fromID int64) (int64, error) {
	// 同事务串行认领，所有入口（包括重放/测试/补偿）遵守同一锁。不能先查待记账行再上锁。
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(1953325673)"); err != nil {
		return 0, err
	}
	var minID, maxID *int64
	var n int64
	if err := tx.QueryRow(ctx, `SELECT min(sub2api_usage_id), max(sub2api_usage_id), count(*) FROM usage_records
		WHERE ledger_txn_id IS NULL AND channel <> 'unknown' AND sub2api_usage_id > $1`, fromID).Scan(&minID, &maxID, &n); err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, nil
	}
	var txnID int64
	if err := tx.QueryRow(ctx, `INSERT INTO ledger_txns (type, ref) VALUES ('usage', $1) RETURNING id`,
		fmt.Sprintf("usage:%d-%d:%d", *minID, *maxID, time.Now().UnixNano())).Scan(&txnID); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `UPDATE usage_records SET ledger_txn_id = $1
		WHERE ledger_txn_id IS NULL AND channel <> 'unknown' AND sub2api_usage_id > $2`, txnID, fromID); err != nil {
		return 0, err
	}
	if err := allocateStoreCredits(ctx, tx, txnID); err != nil {
		return 0, err
	}
	// 确保账本账户存在
	if _, err := tx.Exec(ctx, `INSERT INTO ledger_accounts (owner_type, owner_id, kind)
		SELECT DISTINCT 'buyer', buyer_user_id, 'buyer_cash' FROM usage_records WHERE ledger_txn_id = $1 AND buyer_user_id IS NOT NULL
		UNION SELECT DISTINCT 'buyer', buyer_user_id, 'buyer_store_credit' FROM usage_records WHERE ledger_txn_id = $1 AND buyer_user_id IS NOT NULL
		UNION SELECT DISTINCT 'seller', seller_id, 'seller_pending' FROM usage_records WHERE ledger_txn_id = $1 AND seller_id IS NOT NULL
		UNION SELECT 'platform', 0, 'platform_revenue'
		UNION SELECT 'platform', 0, 'platform_cash'
		UNION SELECT 'platform', 0, 'platform_probe_cost'
		ON CONFLICT DO NOTHING`, txnID); err != nil {
		return 0, err
	}
	// 额度包 Key 的用量：购买时已账面预付，不再扣 buyer_cash；从 platform_cash 释放预付并记卖家/收入。
	if _, err := tx.Exec(ctx, `INSERT INTO ledger_entries (txn_id, account_id, amount)
		SELECT $1, a.id, x.amount FROM (
			SELECT 'buyer' AS ot, ur.buyer_user_id AS oid, 'buyer_cash' AS kind, sum(COALESCE(ur.cash_cost, ur.actual_cost)) AS amount
				FROM usage_records ur
				WHERE ur.ledger_txn_id = $1 AND ur.channel IN ('pool', 'direct') AND ur.buyer_user_id IS NOT NULL
					AND NOT EXISTS (
						SELECT 1 FROM quota_packs qp
						WHERE qp.buyer_key_id = ur.buyer_key_id AND qp.status IN ('active', 'depleted')
					)
				GROUP BY ur.buyer_user_id
			UNION ALL
			SELECT 'buyer', buyer_user_id, 'buyer_store_credit', sum(credit_prepaid_cny)
				FROM usage_records WHERE ledger_txn_id=$1 AND credit_pack_id IS NOT NULL GROUP BY buyer_user_id
			UNION ALL
			SELECT 'platform', 0, 'platform_cash', sum(ur.actual_cost)
				FROM usage_records ur
				WHERE ur.ledger_txn_id = $1 AND ur.channel IN ('pool', 'direct') AND ur.buyer_user_id IS NOT NULL
					AND EXISTS (
						SELECT 1 FROM quota_packs qp
						WHERE qp.buyer_key_id = ur.buyer_key_id AND qp.status IN ('active', 'depleted')
					)
			UNION ALL
			SELECT 'seller', seller_id, 'seller_pending', -sum(payout)
				FROM usage_records WHERE ledger_txn_id = $1 AND seller_id IS NOT NULL GROUP BY seller_id
			UNION ALL
			SELECT 'platform', 0, 'platform_revenue',
				-(COALESCE(sum(COALESCE(cash_cost,actual_cost)+credit_prepaid_cny) FILTER (WHERE buyer_user_id IS NOT NULL), 0) - COALESCE(sum(payout) FILTER (WHERE seller_id IS NOT NULL), 0))
				FROM usage_records WHERE ledger_txn_id = $1 AND channel IN ('pool', 'direct')
			UNION ALL
			SELECT 'platform', 0, 'platform_probe_cost', sum(payout)
				FROM usage_records WHERE ledger_txn_id = $1 AND channel = 'probe' AND seller_id IS NOT NULL
		) x JOIN ledger_accounts a ON a.owner_type = x.ot AND a.owner_id = x.oid AND a.kind = x.kind
		WHERE x.amount IS NOT NULL AND x.amount <> 0`, txnID); err != nil {
		return 0, err
	}
	// 扣减额度包剩余；用尽则标 depleted
	if _, err := tx.Exec(ctx, `UPDATE quota_packs qp SET
			remaining = GREATEST(qp.remaining - u.spent, 0),
			status = CASE WHEN qp.remaining - u.spent <= 0 THEN 'depleted' ELSE qp.status END
		FROM (
			SELECT buyer_key_id, sum(actual_cost)::numeric AS spent
			FROM usage_records
			WHERE ledger_txn_id = $1 AND buyer_key_id IS NOT NULL AND channel IN ('pool', 'direct')
			GROUP BY buyer_key_id
		) u
		WHERE qp.buyer_key_id = u.buyer_key_id AND qp.status = 'active'`, txnID); err != nil {
		return 0, err
	}
	// 汇总到"上游 × 小时 × 渠道"的结算行
	if _, err := tx.Exec(ctx, `INSERT INTO settlement_lines (upstream_id, seller_id, hour, channel, request_count, total_cost, payout, buyer_paid)
		SELECT upstream_id, seller_id, date_trunc('hour', created_at), channel, count(*), sum(total_cost), sum(payout),
			sum(CASE WHEN channel IN ('pool', 'direct') THEN COALESCE(cash_cost,actual_cost)+credit_prepaid_cny ELSE 0 END)
		FROM usage_records WHERE ledger_txn_id = $1 AND upstream_id IS NOT NULL
		GROUP BY upstream_id, seller_id, date_trunc('hour', created_at), channel
		ON CONFLICT (upstream_id, hour, channel) DO UPDATE SET
			request_count = settlement_lines.request_count + EXCLUDED.request_count,
			total_cost = settlement_lines.total_cost + EXCLUDED.total_cost,
			payout = settlement_lines.payout + EXCLUDED.payout,
			buyer_paid = settlement_lines.buyer_paid + EXCLUDED.buyer_paid`, txnID); err != nil {
		return 0, err
	}
	return n, nil
}

// Run 循环入账，有积压时连续拉取。
func (in *Ingester) Run(ctx context.Context, interval time.Duration) {
	for {
		if in.Gate != nil {
			if ok, _ := in.Gate(ctx); !ok {
				select {
				case <-ctx.Done():
					return
				case <-time.After(interval):
				}
				continue
			}
		}
		res, more, err := in.RunOnce(ctx)
		if err != nil && ctx.Err() == nil {
			in.Log.Error("usage ingest", "err", err)
		} else if res != nil && res.Inserted > 0 {
			in.Log.Info("usage ingested", "fetched", res.Fetched, "inserted", res.Inserted, "posted", res.Posted)
		}
		wait := interval
		if more {
			wait = 0
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// Unfreeze 兼容旧的按天调用方；新代码使用 UnfreezeHours。
func Unfreeze(ctx context.Context, db *pgxpool.Pool, freezeDays int, now time.Time) (int, error) {
	if freezeDays < 0 || freezeDays > int((1<<63-1)/int64(time.Hour)-1)/24 {
		return 0, fmt.Errorf("freeze days outside supported range: %d", freezeDays)
	}
	return UnfreezeHours(ctx, db, freezeDays*24, now)
}

// UnfreezeHours 把冻结期已满的卖家收益从待结算转为可提现。
// 按完整小时结束后再等待冻结期，避免同小时靠后的用量被提前释放。
func UnfreezeHours(ctx context.Context, db *pgxpool.Pool, freezeHours int, now time.Time) (int, error) {
	if freezeHours < 0 || freezeHours > int((1<<63-1)/int64(time.Hour))-1 {
		return 0, fmt.Errorf("freeze hours outside supported range: %d", freezeHours)
	}
	cutoff := now.Add(-time.Duration(freezeHours)*time.Hour - time.Hour)
	rows, err := db.Query(ctx, `SELECT seller_id FROM settlement_lines WHERE hour <= $1 AND payout > unfrozen_payout GROUP BY seller_id ORDER BY seller_id`, cutoff)
	if err != nil {
		return 0, err
	}
	sellers, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return 0, err
	}
	done := 0
	for _, sellerID := range sellers {
		released, err := unfreezeSeller(ctx, db, sellerID, cutoff, now)
		if err != nil {
			return done, fmt.Errorf("unfreeze seller %d: %w", sellerID, err)
		}
		if released {
			done++
		}
	}
	return done, nil
}

func unfreezeSeller(ctx context.Context, db *pgxpool.Pool, sellerID int64, cutoff, now time.Time) (bool, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var amount string
	var released bool
	var txID int64
	// 行锁保护认领前的差额，UPDATE 与账本写入共用事务；等待锁的并发轮次
	// 会重新检查已提交的 unfrozen_payout，不会再次认领同一笔收益。
	if err := tx.QueryRow(ctx, `WITH eligible AS MATERIALIZED (
		SELECT upstream_id, hour, channel, payout - unfrozen_payout AS amount
		FROM settlement_lines
		WHERE seller_id = $1 AND hour <= $2 AND payout > unfrozen_payout
		ORDER BY upstream_id, hour, channel FOR UPDATE
	), released AS (
		UPDATE settlement_lines sl SET unfrozen_payout = sl.payout
		FROM eligible e
		WHERE sl.upstream_id = e.upstream_id AND sl.hour = e.hour AND sl.channel = e.channel
		RETURNING e.amount
	)
	SELECT COALESCE(sum(amount), 0)::text, COALESCE(sum(amount), 0) > 0, txid_current()
	FROM released`, sellerID, cutoff).Scan(&amount, &released, &txID); err != nil {
		return false, err
	}
	if !released {
		return false, nil
	}
	if _, err := Post(ctx, tx, "unfreeze", fmt.Sprintf("unfreeze:%d:%d:%d", sellerID, now.UnixNano(), txID), "", nil, []Entry{
		{Kind: KindSellerPending, OwnerID: sellerID, Amount: amount},
		{Kind: KindSellerAvailable, OwnerID: sellerID, Amount: "-" + amount},
	}); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}
