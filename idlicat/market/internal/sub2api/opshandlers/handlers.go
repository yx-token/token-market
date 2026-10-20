// Package opshandlers 把出站表中的操作类型映射到 sub2api Admin API 调用。
// 只有这里知道每种操作的 payload 结构；业务代码通过对应的 Payload 类型入队。
package opshandlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/yx-token/idlicat/market/internal/sub2api/adminclient"
	"github.com/yx-token/idlicat/market/internal/sub2api/gateway"
	"github.com/yx-token/idlicat/market/internal/sub2api/ops"
)

const (
	TypeUserBalanceAdd        = "user.balance_add"
	TypeUserBalanceSubtract   = "user.balance_subtract"
	TypeAccountCreate         = "account.create"
	TypeAccountUpdate         = "account.update"
	TypeAccountBulkUpdate     = "account.bulk_update"
	TypeAccountExtraMerge     = "account.extra_merge"
	TypeAccountResyncRecover  = "account.resync_recover"
	TypeAccountSetSchedulable = "account.set_schedulable"
	TypeAccountDelete         = "account.delete"
	TypeGroupUpdate           = "group.update"
	TypeGroupDelete           = "group.delete"
	TypeUserUpdate            = "user.update"
)

type BalancePayload struct {
	UserID int64   `json:"user_id"`
	Amount float64 `json:"amount"`
	Notes  string  `json:"notes,omitempty"`
}

type AccountCreatePayload struct {
	Input adminclient.CreateAccountInput `json:"input"`
}

type AccountUpdatePayload struct {
	AccountID int64                          `json:"account_id"`
	Input     adminclient.UpdateAccountInput `json:"input"`
}

// AccountExtraMergePayload 修改账号 extra 中的若干键。sub2api 的批量更新会静默丢弃自动用卡配置，
// 只能用 PUT 整体替换 extra，所以发送时先读出账号当前的 extra 再合并，尽量缩短与 sub2api 自身写入的竞争窗口。
type AccountExtraMergePayload struct {
	AccountID int64          `json:"account_id"`
	Set       map[string]any `json:"set"`
}

// AccountResyncRecoverPayload 向官方重新查询账号用量（回写 sub2api 的用量快照）后恢复账号状态。
// 用于 Claude 用卡之后：sub2api 不会自行恢复，旧快照还会让窗口阈值继续暂停调度。
type AccountResyncRecoverPayload struct {
	AccountID int64 `json:"account_id"`
}

type AccountSchedulablePayload struct {
	AccountID   int64 `json:"account_id"`
	Schedulable bool  `json:"schedulable"`
}

type AccountDeletePayload struct {
	AccountID int64 `json:"account_id"`
}

type GroupUpdatePayload struct {
	GroupID int64                        `json:"group_id"`
	Input   adminclient.UpdateGroupInput `json:"input"`
}

type GroupDeletePayload struct {
	GroupID int64 `json:"group_id"`
}

type UserUpdatePayload struct {
	UserID int64                       `json:"user_id"`
	Input  adminclient.UpdateUserInput `json:"input"`
}

// Register 把全部处理器注册到发送器。
// c 一般为 gateway.HTTP(adminClient)；进程内实现也可直接传入。
func Register(s *ops.Sender, c gateway.Admin) {
	s.Register(TypeUserBalanceAdd, balance(c, adminclient.BalanceAdd))
	s.Register(TypeUserBalanceSubtract, balance(c, adminclient.BalanceSubtract))
	s.Register(TypeAccountCreate, handle(func(ctx context.Context, op ops.Op, p AccountCreatePayload) (any, error) {
		return c.CreateAccount(ctx, p.Input, op.IdempotencyKey)
	}))
	s.Register(TypeAccountUpdate, handle(func(ctx context.Context, _ ops.Op, p AccountUpdatePayload) (any, error) {
		return c.UpdateAccount(ctx, p.AccountID, p.Input)
	}))
	s.Register(TypeAccountBulkUpdate, handle(func(ctx context.Context, _ ops.Op, p adminclient.BulkUpdateAccountsInput) (any, error) {
		return c.BulkUpdateAccounts(ctx, p)
	}))
	s.Register(TypeAccountExtraMerge, handle(func(ctx context.Context, _ ops.Op, p AccountExtraMergePayload) (any, error) {
		a, err := c.GetAccount(ctx, p.AccountID)
		if err != nil {
			return nil, err
		}
		extra := make(map[string]any, len(a.Extra)+len(p.Set))
		for k, v := range a.Extra {
			extra[k] = v
		}
		for k, v := range p.Set {
			extra[k] = v
		}
		return c.UpdateAccount(ctx, p.AccountID, adminclient.UpdateAccountInput{Extra: extra})
	}))
	s.Register(TypeAccountResyncRecover, handle(func(ctx context.Context, _ ops.Op, p AccountResyncRecoverPayload) (any, error) {
		_, err := c.AccountUsage(ctx, p.AccountID, false)
		if err == nil {
			err = c.RecoverAccountState(ctx, p.AccountID)
		}
		if adminclient.StatusOf(err) == 404 {
			return nil, nil // 账号已删除
		}
		return nil, err
	}))
	s.Register(TypeAccountSetSchedulable, handle(func(ctx context.Context, _ ops.Op, p AccountSchedulablePayload) (any, error) {
		return nil, c.SetSchedulable(ctx, p.AccountID, p.Schedulable)
	}))
	s.Register(TypeAccountDelete, handle(func(ctx context.Context, _ ops.Op, p AccountDeletePayload) (any, error) {
		err := c.DeleteAccount(ctx, p.AccountID)
		if adminclient.StatusOf(err) == 404 {
			return nil, nil // 已经不存在，视为成功
		}
		return nil, err
	}))
	s.Register(TypeGroupUpdate, handle(func(ctx context.Context, _ ops.Op, p GroupUpdatePayload) (any, error) {
		return c.UpdateGroup(ctx, p.GroupID, p.Input)
	}))
	s.Register(TypeGroupDelete, handle(func(ctx context.Context, _ ops.Op, p GroupDeletePayload) (any, error) {
		err := c.DeleteGroup(ctx, p.GroupID)
		if adminclient.StatusOf(err) == 404 {
			return nil, nil
		}
		return nil, err
	}))
	s.Register(TypeUserUpdate, handle(func(ctx context.Context, _ ops.Op, p UserUpdatePayload) (any, error) {
		return c.UpdateUser(ctx, p.UserID, p.Input)
	}))
}

func balance(c gateway.Admin, op adminclient.BalanceOperation) ops.Handler {
	return handle(func(ctx context.Context, o ops.Op, p BalancePayload) (any, error) {
		// 幂等键直接复用出站记录的键：重试时 sub2api 返回首次结果而不会重复加减
		return c.UpdateBalance(ctx, p.UserID, op, p.Amount, p.Notes, o.IdempotencyKey)
	})
}

// handle 负责解码 payload，解码失败属于不可重试的编程错误。
func handle[P any](fn func(context.Context, ops.Op, P) (any, error)) ops.Handler {
	return func(ctx context.Context, op ops.Op) (any, error) {
		var p P
		if err := json.Unmarshal(op.Payload, &p); err != nil {
			return nil, &permanentError{fmt.Errorf("decode %s payload: %w", op.Type, err)}
		}
		return fn(ctx, op, p)
	}
}

type permanentError struct{ err error }

func (e *permanentError) Error() string { return e.err.Error() }
func (e *permanentError) Unwrap() error { return e.err }

// Retryable 组合 sub2api 的错误分类与本包的不可重试错误，作为 ops.SenderConfig.Retryable。
func Retryable(err error) bool {
	var perm *permanentError
	if errors.As(err, &perm) {
		return false
	}
	return adminclient.Retryable(err)
}
