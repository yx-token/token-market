package gateway

import (
	"context"
	"encoding/json"

	"github.com/yx-token/token-market/market/internal/sub2api/adminclient"
)

// Admin 覆盖出站 handlers 所需的 Admin API 写面。
// *adminclient.Client 直接满足本接口；进程内实现日后可替换。
type Admin interface {
	UpdateBalance(ctx context.Context, userID int64, op adminclient.BalanceOperation, amount float64, notes, idempotencyKey string) (*adminclient.User, error)
	UpdateUser(ctx context.Context, id int64, in adminclient.UpdateUserInput) (*adminclient.User, error)

	CreateAccount(ctx context.Context, in adminclient.CreateAccountInput, idempotencyKey string) (*adminclient.Account, error)
	UpdateAccount(ctx context.Context, id int64, in adminclient.UpdateAccountInput) (*adminclient.Account, error)
	BulkUpdateAccounts(ctx context.Context, in adminclient.BulkUpdateAccountsInput) (json.RawMessage, error)
	GetAccount(ctx context.Context, id int64) (*adminclient.Account, error)
	SetSchedulable(ctx context.Context, id int64, schedulable bool) error
	DeleteAccount(ctx context.Context, id int64) error
	AccountUsage(ctx context.Context, id int64, passive bool) (*adminclient.AccountUsage, error)
	RecoverAccountState(ctx context.Context, id int64) error

	UpdateGroup(ctx context.Context, id int64, in adminclient.UpdateGroupInput) (*adminclient.Group, error)
	DeleteGroup(ctx context.Context, id int64) error
}

// HTTP 把现有 Admin HTTP 客户端适配为 Admin 端口。
func HTTP(c *adminclient.Client) Admin {
	if c == nil {
		return nil
	}
	return c
}
