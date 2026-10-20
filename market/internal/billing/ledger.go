// Package billing 负责资金：用量入账、复式账本、卖家结算与解冻、充值、退款、提现、计费对账、Key 花费限额。
//
// 金额计算尽量在 SQL 中用 numeric 完成，避免浮点误差；Go 侧只在展示时转换为 float64。
package billing

import (
	"context"
	"errors"
	"fmt"

	"github.com/yx-token/token-market/market/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type DB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// 账户类型。负债类（买家余额、卖家收益）的余额 = −Σ分录，资产与费用类 = Σ分录。
const (
	KindBuyerCash          = "buyer_cash"
	KindSellerPending      = "seller_pending"
	KindSellerAvailable    = "seller_available"
	KindSellerWithdrawing  = "seller_withdrawing"
	KindPlatformCash       = "platform_cash"
	KindPlatformRevenue    = "platform_revenue"
	KindPlatformProbeCost  = "platform_probe_cost"
	KindPlatformAdjustment = "platform_adjustment"
)

func ownerTypeOf(kind string) string {
	switch kind {
	case KindBuyerCash, KindBuyerStoreCredit, "buyer_bonus":
		return "buyer"
	case KindSellerPending, KindSellerAvailable, KindSellerWithdrawing:
		return "seller"
	default:
		return "platform"
	}
}

// AccountID 返回（必要时创建）账本账户。
func AccountID(ctx context.Context, db DB, kind string, ownerID int64) (int64, error) {
	ownerType := ownerTypeOf(kind)
	if ownerType == "platform" {
		ownerID = 0
	}
	var id int64
	err := db.QueryRow(ctx, `INSERT INTO ledger_accounts (owner_type, owner_id, kind) VALUES ($1, $2, $3)
		ON CONFLICT (owner_type, owner_id, kind) DO UPDATE SET kind = EXCLUDED.kind RETURNING id`, ownerType, ownerID, kind).Scan(&id)
	return id, err
}

// Entry 是一笔分录。Amount 为十进制字符串（正为借、负为贷）。
type Entry struct {
	Kind    string
	OwnerID int64
	Amount  string
}

var ErrDuplicateTxn = errors.New("billing: duplicate ledger transaction")

// Post 在给定事务中记一笔交易。同一 (type, ref) 只能记一次，重复时返回 ErrDuplicateTxn。
// 借贷平衡由数据库的延迟约束触发器在提交时校验。
func Post(ctx context.Context, tx pgx.Tx, typ, ref, note string, createdBy *int64, entries []Entry) (int64, error) {
	var txnID int64
	err := tx.QueryRow(ctx, `INSERT INTO ledger_txns (type, ref, note, created_by) VALUES ($1, $2, NULLIF($3, ''), $4)
		ON CONFLICT (type, ref) DO NOTHING RETURNING id`, typ, ref, note, createdBy).Scan(&txnID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrDuplicateTxn
	}
	if err != nil {
		return 0, err
	}
	for _, e := range entries {
		acc, err := AccountID(ctx, tx, e.Kind, e.OwnerID)
		if err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO ledger_entries (txn_id, account_id, amount) SELECT $1, $2, $3::numeric WHERE $3::numeric <> 0`,
			txnID, acc, e.Amount); err != nil {
			return 0, fmt.Errorf("ledger entry %s: %w", e.Kind, err)
		}
	}
	return txnID, nil
}

// Balances 返回某个所有者各账户的余额（负债类已取反为正数）。
func Balances(ctx context.Context, db DB, ownerType string, ownerID int64) (map[string]float64, error) {
	rows, err := db.Query(ctx, `SELECT a.kind, COALESCE(sum(e.amount), 0)::float8
		FROM ledger_accounts a LEFT JOIN ledger_entries e ON e.account_id = a.id
		WHERE a.owner_type = $1 AND a.owner_id = $2 GROUP BY a.kind`, ownerType, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]float64{}
	for rows.Next() {
		var kind string
		var sum float64
		if err := rows.Scan(&kind, &sum); err != nil {
			return nil, err
		}
		switch ownerTypeOf(kind) {
		case "buyer", "seller":
			out[kind] = -sum
		default:
			if kind == KindPlatformRevenue {
				out[kind] = -sum
			} else {
				out[kind] = sum
			}
		}
	}
	return out, rows.Err()
}

// balanceExact 返回负债类账户的精确余额（十进制字符串），用于提现等需要精确比较的场景。
func balanceExact(ctx context.Context, db DB, kind string, ownerID int64) (string, error) {
	var s string
	err := db.QueryRow(ctx, `SELECT (-COALESCE(sum(e.amount), 0))::text FROM ledger_accounts a LEFT JOIN ledger_entries e ON e.account_id = a.id
		WHERE a.owner_type = $1 AND a.owner_id = $2 AND a.kind = $3`, ownerTypeOf(kind), ownerID, kind).Scan(&s)
	return s, err
}

// ValidAmount 校验金额：正数、最多 2 位小数，返回标准十进制字符串。
func ValidAmount(v float64) (string, error) {
	if v <= 0 || v > 1e9 {
		return "", httpx.BadRequest("INVALID_AMOUNT", "amount must be positive")
	}
	s := fmt.Sprintf("%.2f", v)
	var back float64
	if _, err := fmt.Sscanf(s, "%f", &back); err != nil || abs(back-v) > 1e-9 {
		return "", httpx.BadRequest("INVALID_AMOUNT", "amount supports at most 2 decimal places")
	}
	return s, nil
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
