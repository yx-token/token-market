// Package enginehost 给 sub2api 进程调用闲猫账本与申诉裁决。
// 实现仍是 billing.Funds / quality.Engine，不另写一套入账 SQL。
package enginehost

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/yx-token/token-market/market/internal/app"
	"github.com/yx-token/token-market/market/internal/httpapi"
	"github.com/yx-token/token-market/market/internal/platform/config"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
)

// APIError 是可直接返回给管理端的业务错误。
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string { return e.Code + ": " + e.Message }

var (
	once sync.Once
	a    *app.App
	open error
)

func market(ctx context.Context) (*app.App, error) {
	once.Do(func() {
		cfg, err := config.Load("")
		if err != nil {
			open = err
			return
		}
		a, open = app.Build(context.Background(), cfg, slog.Default())
		if open != nil {
			slog.Error("enginehost", "err", open)
		}
	})
	return a, open
}

func wrap(err error) error {
	if err == nil {
		return nil
	}
	var he *httpx.Error
	if errors.As(err, &he) {
		return &APIError{Status: he.Status, Code: he.Code, Message: he.Message}
	}
	return err
}

func ConfirmRecharge(ctx context.Context, orderID int64, providerRef string, confirmedBy *int64) (any, error) {
	m, err := market(ctx)
	if err != nil {
		return nil, err
	}
	out, err := m.Funds.ConfirmRecharge(ctx, orderID, providerRef, confirmedBy)
	return out, wrap(err)
}

func RefundRecharge(ctx context.Context, orderID int64, amount float64, note string) (any, error) {
	m, err := market(ctx)
	if err != nil {
		return nil, err
	}
	out, err := m.Funds.RefundRechargeOrder(ctx, 0, orderID, amount, note)
	return out, wrap(err)
}

func MarkRefundPaid(ctx context.Context, refundID int64, payoutRef string) (any, error) {
	m, err := market(ctx)
	if err != nil {
		return nil, err
	}
	out, err := m.Funds.MarkRefundPaid(ctx, refundID, payoutRef)
	return out, wrap(err)
}

func ReviewWithdrawal(ctx context.Context, adminID, id int64, approve bool, reason string) (any, error) {
	m, err := market(ctx)
	if err != nil {
		return nil, err
	}
	out, err := m.Funds.ReviewWithdrawal(ctx, adminID, id, approve, reason)
	return out, wrap(err)
}

func MarkWithdrawalPaid(ctx context.Context, adminID, id int64, payoutRef string, failed bool, reason string) (any, error) {
	m, err := market(ctx)
	if err != nil {
		return nil, err
	}
	out, err := m.Funds.MarkWithdrawalPaid(ctx, adminID, id, payoutRef, failed, reason)
	return out, wrap(err)
}

// ServeAdmin 执行尚未在 idlecat 里单独实现的管理接口，响应体与 market-api 一致。
func ServeAdmin(ctx context.Context, method, rest, rawQuery string, body []byte, adminID int64) (int, []byte, error) {
	m, err := market(ctx)
	if err != nil {
		return 0, nil, err
	}
	status, payload := httpapi.ServeAdmin(m, method, rest, rawQuery, body, adminID)
	return status, payload, nil
}

func ResolveAppeal(ctx context.Context, appealID int64, action, note string) (any, error) {
	m, err := market(ctx)
	if err != nil {
		return nil, err
	}
	out, err := m.Quality.ResolveAppeal(ctx, appealID, action, note)
	return out, wrap(err)
}
