//go:build unit

package repository

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestKimiOAuthMutationsHaveCredentialCASAndAtomicOutbox(t *testing.T) {
	for _, operation := range []string{"credentials", "permanent", "temporary", "recover"} {
		t.Run(operation, func(t *testing.T) {
			exec := &recordingSQLExecutor{result: rowsAffectedResult(0)}
			repo := newAccountRepositoryWithSQL(nil, exec, nil)
			expected := map[string]any{"refresh_token": "r", "_token_version": 42}
			proxy := int64(12)
			var applied bool
			var err error
			switch operation {
			case "credentials":
				applied, err = repo.UpdateKimiOAuthCredentialsIfUnchanged(context.Background(), 1, expected, &proxy, map[string]any{"refresh_token": "rotated"})
			case "recover":
				applied, err = repo.RecoverKimiOAuthIfUnchanged(context.Background(), 1, expected, &proxy)
			default:
				applied, err = repo.SetKimiOAuthFailureIfUnchanged(context.Background(), 1, expected, &proxy, operation == "permanent", time.Now().Add(time.Minute), "reauthorize")
			}
			require.NoError(t, err)
			require.False(t, applied)
			require.Len(t, exec.execQueries, 1)
			query := normalizeSQLWhitespace(exec.execQueries[0])
			require.Contains(t, query, "a.platform = 'kimi' AND a.type = 'oauth'")
			require.Contains(t, query, "a.credentials = $6::jsonb AND a.proxy_id IS NOT DISTINCT FROM $7")
			require.Contains(t, query, "INSERT INTO scheduler_outbox")
			require.Equal(t, operation, exec.execArgs[0][0])
		})
	}
}
