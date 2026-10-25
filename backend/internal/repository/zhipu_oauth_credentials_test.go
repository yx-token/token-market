//go:build unit

package repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"github.com/yx-token/token-market/internal/service"
)

func TestZhipuOAuthCredentialsConditionalPersistence(t *testing.T) {
	for _, success := range []bool{false, true} {
		for _, rows := range []int64{0, 1} {
			t.Run(map[bool]string{false: "reauthorization", true: "credentials"}[success]+string(rune('0'+rows)), func(t *testing.T) {
				db, mock, err := sqlmock.New()
				require.NoError(t, err)
				t.Cleanup(func() { _ = db.Close() })
				repo := newAccountRepositoryWithSQL(nil, db, nil)
				payload := "{}"
				if success {
					payload = `{"api_key":"new.secret"}`
				}
				mock.ExpectExec(`(?s)WITH updated AS .*UPDATE accounts SET.*WHERE id=\$4 AND deleted_at IS NULL AND platform='zhipu' AND type='oauth'.*AND credentials=\$5::jsonb AND proxy_id IS NOT DISTINCT FROM \$6.*INSERT INTO scheduler_outbox`).WithArgs(success, payload, "reauthorize", int64(41), `{"api_key":"old.secret"}`, int64(7), service.SchedulerOutboxEventAccountChanged).WillReturnResult(sqlmock.NewResult(0, rows))
				proxy := int64(7)
				var credentials map[string]any
				if success {
					credentials = map[string]any{"api_key": "new.secret"}
				}
				applied, err := repo.updateZhipuOAuthState(context.Background(), 41, map[string]any{"api_key": "old.secret"}, &proxy, credentials, "reauthorize")
				require.NoError(t, err)
				require.Equal(t, rows == 1, applied)
				require.NoError(t, mock.ExpectationsWereMet())
			})
		}
	}
}
