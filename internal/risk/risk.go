// Package risk 是资金风控的基础规则：买卖双方关联识别、新卖家集中度、提现审核分级。
package risk

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/DueGin/token-market/internal/platform/httpx"
	"github.com/DueGin/token-market/internal/settings"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	DB       *pgxpool.Pool
	Settings func(ctx context.Context) (settings.Settings, error)
}

// WithdrawalNeedsReview 有未处理的风险事件或处于新卖家期时需要人工审核。
func (s *Service) WithdrawalNeedsReview(ctx context.Context, sellerID int64) (bool, string, error) {
	var open int
	if err := s.DB.QueryRow(ctx, `SELECT count(*) FROM risk_events WHERE status = 'open' AND (user_id = $1 OR related_user_id = $1)`, sellerID).Scan(&open); err != nil {
		return false, "", err
	}
	if open > 0 {
		return true, fmt.Sprintf("%d open risk events", open), nil
	}
	st, err := s.Settings(ctx)
	if err != nil {
		return false, "", err
	}
	var createdAt time.Time
	if err := s.DB.QueryRow(ctx, "SELECT created_at FROM users WHERE id = $1", sellerID).Scan(&createdAt); err != nil {
		return false, "", err
	}
	if time.Since(createdAt) < time.Duration(st.NewSellerDays)*24*time.Hour {
		return true, "new seller", nil
	}
	return false, "", nil
}

// Detect 运行风控规则，返回新增的风险事件数。
func (s *Service) Detect(ctx context.Context) (int64, error) {
	st, err := s.Settings(ctx)
	if err != nil {
		return 0, err
	}
	var total int64
	// 买家指定卖家的流量中，买卖双方使用过相同 IP
	tag, err := s.DB.Exec(ctx, `INSERT INTO risk_events (kind, severity, user_id, related_user_id, dedupe_key, detail)
		SELECT 'buyer_seller_linkage', 'high', pair.seller_id, pair.buyer_user_id,
			'link:' || pair.buyer_user_id || ':' || pair.seller_id, jsonb_build_object('shared_ip', min(bi.ip))
		FROM (SELECT DISTINCT seller_id, buyer_user_id FROM usage_records
			WHERE channel = 'direct' AND seller_id IS NOT NULL AND buyer_user_id IS NOT NULL
				AND created_at > now() - interval '30 days' AND seller_id <> buyer_user_id) pair
		JOIN user_ip_events bi ON bi.user_id = pair.buyer_user_id
		JOIN user_ip_events si ON si.user_id = pair.seller_id AND si.ip = bi.ip
		GROUP BY pair.seller_id, pair.buyer_user_id
		ON CONFLICT (dedupe_key) DO NOTHING`)
	if err != nil {
		return 0, fmt.Errorf("linkage: %w", err)
	}
	total += tag.RowsAffected()
	// 买家与卖家是同一人
	tag, err = s.DB.Exec(ctx, `INSERT INTO risk_events (kind, severity, user_id, related_user_id, dedupe_key)
		SELECT DISTINCT 'self_dealing', 'high', seller_id, buyer_user_id, 'self:' || seller_id
		FROM usage_records WHERE channel = 'direct' AND seller_id = buyer_user_id AND created_at > now() - interval '30 days'
		ON CONFLICT (dedupe_key) DO NOTHING`)
	if err != nil {
		return total, fmt.Errorf("self dealing: %w", err)
	}
	total += tag.RowsAffected()
	// 新卖家来自单一买家的指定流量收益超过上限
	tag, err = s.DB.Exec(ctx, `INSERT INTO risk_events (kind, severity, user_id, related_user_id, dedupe_key, detail)
		SELECT 'direct_concentration', 'medium', ur.seller_id, ur.buyer_user_id,
			'conc:' || ur.seller_id || ':' || ur.buyer_user_id, jsonb_build_object('payout', sum(ur.payout))
		FROM usage_records ur JOIN users u ON u.id = ur.seller_id
		WHERE ur.channel = 'direct' AND u.created_at > now() - make_interval(days => $1)
		GROUP BY ur.seller_id, ur.buyer_user_id HAVING sum(ur.payout) > $2
		ON CONFLICT (dedupe_key) DO NOTHING`, st.NewSellerDays, st.NewSellerDirectCap)
	if err != nil {
		return total, fmt.Errorf("concentration: %w", err)
	}
	total += tag.RowsAffected()
	return total, nil
}

type Event struct {
	ID            int64           `json:"id"`
	Kind          string          `json:"kind"`
	Severity      string          `json:"severity"`
	UserID        *int64          `json:"user_id,omitempty"`
	RelatedUserID *int64          `json:"related_user_id,omitempty"`
	UpstreamID    *int64          `json:"upstream_id,omitempty"`
	Detail        json.RawMessage `json:"detail,omitempty"`
	Status        string          `json:"status"`
	CreatedAt     time.Time       `json:"created_at"`
}

func (s *Service) List(ctx context.Context, status string, limit, offset int) ([]Event, error) {
	rows, err := s.DB.Query(ctx, `SELECT id, kind, severity, user_id, related_user_id, upstream_id, detail, status, created_at
		FROM risk_events WHERE $1 = '' OR status = $1 ORDER BY id DESC LIMIT $2 OFFSET $3`, status, limit, offset)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Event, error) {
		var e Event
		err := row.Scan(&e.ID, &e.Kind, &e.Severity, &e.UserID, &e.RelatedUserID, &e.UpstreamID, &e.Detail, &e.Status, &e.CreatedAt)
		return e, err
	})
}

func (s *Service) Resolve(ctx context.Context, adminID, id int64, status string) error {
	if status != "resolved" && status != "ignored" {
		return httpx.BadRequest("INVALID_STATUS", "status must be resolved or ignored")
	}
	tag, err := s.DB.Exec(ctx, `UPDATE risk_events SET status = $2, resolved_at = now(), resolved_by = $3 WHERE id = $1 AND status = 'open'`, id, status, adminID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.NotFound("RISK_EVENT_NOT_OPEN", "risk event not found or already resolved")
	}
	return nil
}
