package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/yx-token/token-market/internal/service"
)

func (r *accountRepository) mutateKimiOAuth(ctx context.Context, id int64, expected map[string]any, proxy *int64, operation string, creds map[string]any, until time.Time, reason string) (bool, error) {
	if r == nil || r.sql == nil {
		return false, errors.New("kimi OAuth SQL repository is not configured")
	}
	expectedJSON, err := json.Marshal(normalizeJSONMap(expected))
	if err != nil {
		return false, err
	}
	credsJSON, err := json.Marshal(normalizeJSONMap(creds))
	if err != nil {
		return false, err
	}
	result, err := r.sql.ExecContext(ctx, `
 WITH updated AS (
 UPDATE accounts AS a SET
 credentials = CASE WHEN $1 = 'credentials' THEN $2::jsonb ELSE a.credentials END,
 status = CASE WHEN $1 = 'permanent' THEN 'error' WHEN $1 = 'recover' AND a.status = 'error' THEN 'active' ELSE a.status END,
 error_message = CASE WHEN $1 = 'permanent' THEN $3 WHEN $1 = 'recover' THEN '' ELSE a.error_message END,
 temp_unschedulable_until = CASE WHEN $1 = 'temporary' THEN GREATEST(a.temp_unschedulable_until,$4) WHEN $1 = 'recover' THEN NULL ELSE a.temp_unschedulable_until END,
 temp_unschedulable_reason = CASE WHEN $1 = 'temporary' AND (a.temp_unschedulable_until IS NULL OR a.temp_unschedulable_until < $4) THEN $3 WHEN $1 = 'recover' THEN NULL ELSE a.temp_unschedulable_reason END,
 rate_limited_at = CASE WHEN $1 = 'recover' THEN NULL ELSE a.rate_limited_at END,
 rate_limit_reset_at = CASE WHEN $1 = 'recover' THEN NULL ELSE a.rate_limit_reset_at END,
 extra = CASE WHEN $1 = 'recover' THEN COALESCE(a.extra, '{}'::jsonb) - 'model_rate_limits' ELSE a.extra END,
 overload_until = CASE WHEN $1 = 'recover' THEN NULL ELSE a.overload_until END,
 updated_at = NOW()
 WHERE a.id = $5 AND a.deleted_at IS NULL AND a.platform = 'kimi' AND a.type = 'oauth'
 AND a.credentials = $6::jsonb AND a.proxy_id IS NOT DISTINCT FROM $7
 AND ($1 IN ('credentials','recover') OR a.status = 'active')
 RETURNING a.id)
 INSERT INTO scheduler_outbox (event_type,account_id,group_id,payload)
 SELECT $8,updated.id,NULL,NULL FROM updated`, operation, string(credsJSON), reason, until, id, string(expectedJSON), proxy, service.SchedulerOutboxEventAccountChanged)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	if err != nil || n == 0 {
		return false, err
	}
	r.syncSchedulerAccountSnapshotDetached(ctx, id)
	return true, nil
}
func (r *accountRepository) UpdateKimiOAuthCredentialsIfUnchanged(ctx context.Context, id int64, expected map[string]any, proxy *int64, creds map[string]any) (bool, error) {
	return r.mutateKimiOAuth(ctx, id, expected, proxy, "credentials", creds, time.Time{}, "")
}
func (r *accountRepository) SetKimiOAuthFailureIfUnchanged(ctx context.Context, id int64, expected map[string]any, proxy *int64, permanent bool, until time.Time, reason string) (bool, error) {
	operation := "temporary"
	if permanent {
		operation = "permanent"
	}
	return r.mutateKimiOAuth(ctx, id, expected, proxy, operation, nil, until, reason)
}
func (r *accountRepository) RecoverKimiOAuthIfUnchanged(ctx context.Context, id int64, expected map[string]any, proxy *int64) (bool, error) {
	return r.mutateKimiOAuth(ctx, id, expected, proxy, "recover", nil, time.Time{}, "")
}
