package enginehost

import (
	"context"
	"net/http"
	"sync"
)

const sharedProxyMutationLock int64 = 0x544d505258

// ProxyMutationCheck checks whether native proxy IDs have active market bindings.
type ProxyMutationCheck func(context.Context, []int64) error

// LockEngineProxyMutation serializes native proxy reads and mutations with
// market allocation. The binding check shares the lock transaction so waiting
// allocation transactions cannot exhaust the pool needed to perform it.
func LockEngineProxyMutation(ctx context.Context) (check ProxyMutationCheck, release func(), err error) {
	if !Enabled() {
		return func(context.Context, []int64) error { return nil }, func() {}, nil
	}
	m, err := market(ctx)
	if err != nil {
		return nil, nil, err
	}
	tx, err := m.DB.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	var once sync.Once
	release = func() { once.Do(func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }) }
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, sharedProxyMutationLock); err != nil {
		release()
		return nil, nil, err
	}
	check = func(checkCtx context.Context, ids []int64) error {
		if len(ids) == 0 {
			return nil
		}
		var bound bool
		if err := tx.QueryRow(checkCtx, `SELECT EXISTS (
			SELECT 1 FROM upstreams u JOIN proxies p ON p.id=u.proxy_id
			WHERE p.sub2api_proxy_id=ANY($1::bigint[]) AND u.status<>'removed'
		)`, ids).Scan(&bound); err != nil {
			return err
		}
		if bound {
			return &APIError{Status: http.StatusConflict, Code: "MARKET_PROXY_IN_USE", Message: "remove all active market upstream bindings before changing or deleting this proxy"}
		}
		return nil
	}
	return check, release, nil
}
