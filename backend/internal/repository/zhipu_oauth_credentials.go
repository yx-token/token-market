package repository

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/yx-token/token-market/internal/service"
)

func (r *accountRepository) UpdateZhipuOAuthCredentialsIfUnchanged(ctx context.Context, id int64, expected map[string]any, proxyID *int64, credentials map[string]any) (bool, error) {
	return r.updateZhipuOAuthState(ctx, id, expected, proxyID, credentials, "")
}

func (r *accountRepository) SetZhipuOAuthReauthorizationRequiredIfUnchanged(ctx context.Context, id int64, expected map[string]any, proxyID *int64, message string) (bool, error) {
	return r.updateZhipuOAuthState(ctx, id, expected, proxyID, nil, message)
}

func (r *accountRepository) updateZhipuOAuthState(ctx context.Context, id int64, expected map[string]any, proxyID *int64, credentials map[string]any, message string) (bool, error) {
	if r.sql == nil {
		return false, errors.New("account repository SQL executor not configured")
	}
	expectedJSON, err := json.Marshal(normalizeJSONMap(expected))
	if err != nil {
		return false, err
	}
	credentialsJSON, err := json.Marshal(normalizeJSONMap(credentials))
	if err != nil {
		return false, err
	}
	result, err := r.sql.ExecContext(ctx, `
WITH updated AS (
 UPDATE accounts SET
 credentials = CASE WHEN $1::boolean THEN $2::jsonb ELSE credentials END,
 status = CASE WHEN $1::boolean THEN status ELSE 'error' END,
 error_message = CASE WHEN $1::boolean THEN error_message ELSE $3 END,
 schedulable = CASE WHEN $1::boolean THEN schedulable ELSE FALSE END,
 extra = CASE WHEN $1::boolean AND credentials->'api_key' IS DISTINCT FROM $2::jsonb->'api_key'
 THEN COALESCE(extra, '{}'::jsonb) - 'upstream_billing_probe' ELSE extra END,
 updated_at = NOW()
 WHERE id=$4 AND deleted_at IS NULL AND platform='zhipu' AND type='oauth'
 AND credentials=$5::jsonb AND proxy_id IS NOT DISTINCT FROM $6
 RETURNING id
)
INSERT INTO scheduler_outbox (event_type,account_id,group_id,payload)
SELECT $7,id,NULL,NULL FROM updated`, credentials != nil, string(credentialsJSON), message, id, string(expectedJSON), proxyID, service.SchedulerOutboxEventAccountChanged)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil || rows == 0 {
		return false, err
	}
	r.syncSchedulerAccountSnapshotDetached(ctx, id)
	return true, nil
}
