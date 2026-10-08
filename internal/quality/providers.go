package quality

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/DueGin/token-market/internal/platform/crypto"
	"github.com/DueGin/token-market/internal/platform/httpx"
	"github.com/DueGin/token-market/internal/quality/thirdparty"
	"github.com/jackc/pgx/v5"
)

// ProviderSettings 是一个第三方检测站在后台的配置与说明。
type ProviderSettings struct {
	thirdparty.Factory
	Enabled        bool           `json:"enabled"`
	BaseURL        string         `json:"base_url"`
	HasAPIKey      bool           `json:"has_api_key"`
	Settings       map[string]any `json:"settings"` // 已保存的可选参数
	MaxRunsPerHour int            `json:"max_runs_per_hour"`
	TimeoutMinutes int            `json:"timeout_minutes"`
	RunsLastHour   int            `json:"runs_last_hour"`
	UpdatedAt      *time.Time     `json:"updated_at,omitempty"`
}

type providerRow struct {
	name           string
	enabled        bool
	baseURL        string
	apiKeyEnc      []byte
	options        map[string]any
	maxRunsPerHour int
	timeoutMinutes int
	updatedAt      time.Time
}

func loadProviderRow(ctx context.Context, db DB, name string) (*providerRow, error) {
	var r providerRow
	var opts []byte
	err := db.QueryRow(ctx, `SELECT name, enabled, base_url, api_key_enc, options, max_runs_per_hour, timeout_minutes, updated_at
		FROM detection_providers WHERE name = $1`, name).Scan(&r.name, &r.enabled, &r.baseURL, &r.apiKeyEnc, &opts, &r.maxRunsPerHour,
		&r.timeoutMinutes, &r.updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(opts, &r.options); err != nil {
		return nil, err
	}
	if r.options == nil {
		r.options = map[string]any{}
	}
	return &r, nil
}

func providerAAD(name string) string { return "detection-provider:" + name }

// ListProviders 返回全部可接入的检测站及其配置；从未保存过的检测站按默认值展示（未启用）。
func ListProviders(ctx context.Context, db DB) ([]ProviderSettings, error) {
	var out []ProviderSettings
	for _, f := range thirdparty.Known() {
		ps := ProviderSettings{Factory: f, BaseURL: f.DefaultBaseURL, Settings: map[string]any{}, MaxRunsPerHour: 20, TimeoutMinutes: 60}
		r, err := loadProviderRow(ctx, db, f.Name)
		if err != nil {
			return nil, err
		}
		if r != nil {
			ps.Enabled, ps.BaseURL, ps.HasAPIKey, ps.Settings = r.enabled, r.baseURL, len(r.apiKeyEnc) > 0, r.options
			ps.MaxRunsPerHour, ps.TimeoutMinutes, ps.UpdatedAt = r.maxRunsPerHour, r.timeoutMinutes, &r.updatedAt
		}
		if err := db.QueryRow(ctx, `SELECT count(*) FROM third_party_checks WHERE provider = $1 AND submitted_at > now() - interval '1 hour'`,
			f.Name).Scan(&ps.RunsLastHour); err != nil {
			return nil, err
		}
		out = append(out, ps)
	}
	return out, nil
}

type ProviderInput struct {
	Enabled        *bool           `json:"enabled"`
	BaseURL        *string         `json:"base_url"`
	APIKey         *string         `json:"api_key"` // 空串表示清除
	Settings       *map[string]any `json:"settings"`
	MaxRunsPerHour *int            `json:"max_runs_per_hour"`
	TimeoutMinutes *int            `json:"timeout_minutes"`
}

// UpsertProvider 保存检测站配置。
func UpsertProvider(ctx context.Context, db DB, kr *crypto.Keyring, name string, in ProviderInput, adminID int64) (*ProviderSettings, error) {
	f, ok := thirdparty.Lookup(name)
	if !ok {
		return nil, httpx.NotFound("PROVIDER_NOT_FOUND", "unknown detection provider")
	}
	r, err := loadProviderRow(ctx, db, name)
	if err != nil {
		return nil, err
	}
	if r == nil {
		r = &providerRow{name: name, baseURL: f.DefaultBaseURL, options: map[string]any{}, maxRunsPerHour: 20, timeoutMinutes: 60}
	}
	if in.Enabled != nil {
		r.enabled = *in.Enabled
	}
	if in.BaseURL != nil {
		r.baseURL = *in.BaseURL
	}
	if in.MaxRunsPerHour != nil {
		r.maxRunsPerHour = *in.MaxRunsPerHour
	}
	if in.TimeoutMinutes != nil {
		r.timeoutMinutes = *in.TimeoutMinutes
	}
	if in.Settings != nil {
		for k := range *in.Settings {
			if _, ok := f.Options[k]; !ok && k != "allow_insecure_target" {
				return nil, httpx.BadRequest("INVALID_OPTION", "unknown option "+k)
			}
		}
		r.options = *in.Settings
	}
	if in.APIKey != nil {
		if *in.APIKey == "" {
			r.apiKeyEnc = nil
		} else if r.apiKeyEnc, err = kr.SealString(*in.APIKey, providerAAD(name)); err != nil {
			return nil, err
		}
	}
	if u, err := url.Parse(r.baseURL); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, httpx.BadRequest("INVALID_BASE_URL", "base_url must be an http(s) url")
	}
	switch {
	case r.maxRunsPerHour < 1 || r.maxRunsPerHour > 1000:
		return nil, httpx.BadRequest("INVALID_RATE", "max_runs_per_hour must be between 1 and 1000")
	case r.timeoutMinutes < 5 || r.timeoutMinutes > 1440:
		return nil, httpx.BadRequest("INVALID_TIMEOUT", "timeout_minutes must be between 5 and 1440")
	}
	opts, _ := json.Marshal(r.options)
	if _, err := db.Exec(ctx, `INSERT INTO detection_providers (name, enabled, base_url, api_key_enc, options, max_runs_per_hour, timeout_minutes, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (name) DO UPDATE SET enabled = EXCLUDED.enabled, base_url = EXCLUDED.base_url, api_key_enc = EXCLUDED.api_key_enc,
			options = EXCLUDED.options, max_runs_per_hour = EXCLUDED.max_runs_per_hour, timeout_minutes = EXCLUDED.timeout_minutes,
			updated_by = EXCLUDED.updated_by, updated_at = now()`,
		name, r.enabled, r.baseURL, r.apiKeyEnc, opts, r.maxRunsPerHour, r.timeoutMinutes, adminID); err != nil {
		return nil, err
	}
	all, err := ListProviders(ctx, db)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].Name == name {
			return &all[i], nil
		}
	}
	return nil, fmt.Errorf("provider %s disappeared", name)
}

// ThirdPartyCheckView 是一次第三方检测的记录（管理后台）。
type ThirdPartyCheckView struct {
	ID          int64           `json:"id"`
	RunID       int64           `json:"run_id"`
	UpstreamID  int64           `json:"upstream_id"`
	Provider    string          `json:"provider"`
	Model       string          `json:"model"`
	Format      string          `json:"format"`
	Status      string          `json:"status"`
	ExternalID  *string         `json:"external_id,omitempty"`
	Outcome     *string         `json:"outcome,omitempty"`
	Verdict     *string         `json:"verdict,omitempty"`
	Summary     json.RawMessage `json:"summary,omitempty"`
	ReportURL   *string         `json:"report_url,omitempty"`
	Error       *string         `json:"error,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	SubmittedAt *time.Time      `json:"submitted_at,omitempty"`
	FinishedAt  *time.Time      `json:"finished_at,omitempty"`
}

func ListThirdPartyChecks(ctx context.Context, db DB, upstreamID int64, limit, offset int) ([]ThirdPartyCheckView, error) {
	rows, err := db.Query(ctx, `SELECT id, run_id, upstream_id, provider, model, format, status, external_id, outcome, verdict, summary, report_url,
			error, created_at, submitted_at, finished_at
		FROM third_party_checks WHERE $1 = 0 OR upstream_id = $1 ORDER BY id DESC LIMIT $2 OFFSET $3`, upstreamID, limit, offset)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (ThirdPartyCheckView, error) {
		var v ThirdPartyCheckView
		err := row.Scan(&v.ID, &v.RunID, &v.UpstreamID, &v.Provider, &v.Model, &v.Format, &v.Status, &v.ExternalID, &v.Outcome, &v.Verdict,
			&v.Summary, &v.ReportURL, &v.Error, &v.CreatedAt, &v.SubmittedAt, &v.FinishedAt)
		return v, err
	})
}
