// Package usermail 按用户 ID 批量解析邮箱，供管理端展示卖家/买家账号。
package usermail

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// DB 是本包需要的最小数据库面。
type DB interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Lookup 返回 id → email；空 ids 返回空 map。
func Lookup(ctx context.Context, db DB, ids []int64) (map[int64]string, error) {
	out := map[int64]string{}
	if len(ids) == 0 {
		return out, nil
	}
	uniq := make([]int64, 0, len(ids))
	seen := map[int64]bool{}
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		uniq = append(uniq, id)
	}
	if len(uniq) == 0 {
		return out, nil
	}
	var b strings.Builder
	args := make([]any, len(uniq))
	b.WriteString(`SELECT id, email FROM users WHERE id IN (`)
	for i, id := range uniq {
		if i > 0 {
			b.WriteByte(',')
		}
		args[i] = id
		fmt.Fprintf(&b, "$%d", i+1)
	}
	b.WriteByte(')')
	rows, err := db.Query(ctx, b.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var email string
		if err := rows.Scan(&id, &email); err != nil {
			return nil, err
		}
		out[id] = email
	}
	return out, rows.Err()
}
