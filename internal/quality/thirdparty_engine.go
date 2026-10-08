package quality

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/DueGin/token-market/internal/edgecfg"
	"github.com/DueGin/token-market/internal/platform/randx"
	"github.com/DueGin/token-market/internal/quality/thirdparty"
	"github.com/jackc/pgx/v5"
)

// 第三方检测
//
// 内部探测完成后，若策略为本次检测类型配置了第三方检测，检测记录进入 third_party 状态并为每个检测站生成一条
// third_party_checks。worker 为每条检测生成一个只路由到该上游探测分组、到期自动失效的 Edge 临时令牌，
// 把 Edge 对外地址与令牌交给检测站；检测站经 Edge 访问被测账号（与内部探测一样计入探测成本）。
// 全部检测站出结果（或超时）后，按策略把检测站的结论折算后与内部判定合并（取更严重者），再统一处置。
// 内部判定为不可用时不再提交第三方检测。

// tpResult 是一条第三方检测的最终结果。
type tpResult struct {
	Provider  string         `json:"provider"`
	Status    string         `json:"status"`
	Outcome   string         `json:"outcome,omitempty"`
	Verdict   string         `json:"verdict"`
	Model     string         `json:"model"`
	ReportURL string         `json:"report_url,omitempty"`
	Error     string         `json:"error,omitempty"`
	Summary   map[string]any `json:"summary,omitempty"`
}

var verdictRanks = map[string]int{"normal": 0, "inconclusive": 1, "suspected": 2, "degraded": 3, "unavailable": 4}

func verdictRank(v string) int { return verdictRanks[v] }

func (e *Engine) thirdPartyClient() *http.Client {
	if e.ThirdPartyHTTP != nil {
		return e.ThirdPartyHTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// startThirdParty 为刚完成内部探测的检测记录排队第三方检测；返回 true 表示需要等待第三方结果。
func (e *Engine) startThirdParty(ctx context.Context, run runRow, u *upstreamRow, p *Policy, a Assessment) (bool, error) {
	if u.isReference || a.Verdict == "unavailable" {
		return false, nil
	}
	type planned struct {
		check   ThirdPartyCheck
		timeout int
	}
	var plan []planned
	for _, c := range p.ThirdParty {
		if !c.Applies(run.trigger) {
			continue
		}
		row, err := loadProviderRow(ctx, e.DB, c.Provider)
		if err != nil {
			return false, err
		}
		if row != nil && row.enabled {
			plan = append(plan, planned{c, row.timeoutMinutes})
		}
	}
	if len(plan) == 0 {
		return false, nil
	}
	tx, err := e.DB.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `UPDATE probe_runs SET status = 'third_party', summary = $2 WHERE id = $1 AND status = 'running'`,
		run.id, mustJSON(map[string]any{"internal": a}))
	if err != nil || tag.RowsAffected() == 0 {
		return false, err
	}
	for _, pl := range plan {
		model := pl.check.Model
		if model == "" {
			model = run.model
		}
		format := pl.check.Format
		if format == "" {
			format = DefaultFormat(u.platform)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO third_party_checks (run_id, upstream_id, provider, model, format, config, deadline)
			VALUES ($1, $2, $3, $4, $5, $6, now() + make_interval(mins => $7))`,
			run.id, u.id, pl.check.Provider, model, format, mustJSON(pl.check), pl.timeout); err != nil {
			return false, err
		}
	}
	return true, tx.Commit(ctx)
}

type tpCheckRow struct {
	id, runID, upstreamID int64
	provider, model       string
	format, status        string
	config                ThirdPartyCheck
	externalID            *string
	attempts              int
	deadline              time.Time
}

// advanceThirdParty 推进第三方检测：超时、提交、轮询、撤销令牌、合并结论。
func (e *Engine) advanceThirdParty(ctx context.Context) error {
	if _, err := e.DB.Exec(ctx, `UPDATE third_party_checks SET status = 'timeout', outcome = 'inconclusive', verdict = 'inconclusive',
			error = 'no result before deadline', finished_at = now()
		WHERE status IN ('pending', 'running') AND deadline < now()`); err != nil {
		return err
	}
	rows, err := e.DB.Query(ctx, `SELECT id, run_id, upstream_id, provider, model, format, status, config, external_id, attempts, deadline
		FROM third_party_checks WHERE status IN ('pending', 'running') AND next_attempt_at <= now() ORDER BY id LIMIT 50`)
	if err != nil {
		return err
	}
	checks, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (tpCheckRow, error) {
		var c tpCheckRow
		var cfg []byte
		if err := row.Scan(&c.id, &c.runID, &c.upstreamID, &c.provider, &c.model, &c.format, &c.status, &cfg, &c.externalID, &c.attempts,
			&c.deadline); err != nil {
			return c, err
		}
		return c, json.Unmarshal(cfg, &c.config)
	})
	if err != nil {
		return err
	}
	for _, c := range checks {
		var err error
		if c.status == "pending" {
			err = e.submitCheck(ctx, c)
		} else {
			err = e.pollCheck(ctx, c)
		}
		if err != nil {
			e.Log.Warn("third-party check", "check_id", c.id, "provider", c.provider, "err", err)
		}
	}
	if err := e.revokeTokens(ctx); err != nil {
		return err
	}
	return e.finishThirdParty(ctx)
}

func (e *Engine) failCheck(ctx context.Context, id int64, status, msg string) error {
	_, err := e.DB.Exec(ctx, `UPDATE third_party_checks SET status = $2, outcome = 'inconclusive', verdict = 'inconclusive', error = $3,
		finished_at = now() WHERE id = $1`, id, status, truncateStr(msg, 500))
	return err
}

func truncateStr(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func (e *Engine) provider(ctx context.Context, name string) (thirdparty.Provider, *providerRow, error) {
	row, err := loadProviderRow(ctx, e.DB, name)
	if err != nil || row == nil {
		return nil, row, err
	}
	cfg := thirdparty.Config{BaseURL: row.baseURL, Options: row.options}
	if len(row.apiKeyEnc) > 0 {
		if cfg.APIKey, err = e.Keyring.OpenString(row.apiKeyEnc, providerAAD(name)); err != nil {
			return nil, row, err
		}
	}
	p, err := thirdparty.New(name, cfg, e.thirdPartyClient())
	return p, row, err
}

func (e *Engine) submitCheck(ctx context.Context, c tpCheckRow) error {
	prov, row, err := e.provider(ctx, c.provider)
	if err != nil {
		return err
	}
	if row == nil || !row.enabled {
		return e.failCheck(ctx, c.id, "skipped", "provider is disabled")
	}
	var recent int
	if err := e.DB.QueryRow(ctx, `SELECT count(*) FROM third_party_checks WHERE provider = $1 AND submitted_at > now() - interval '1 hour'`,
		c.provider).Scan(&recent); err != nil {
		return err
	}
	if recent >= row.maxRunsPerHour {
		_, err := e.DB.Exec(ctx, "UPDATE third_party_checks SET next_attempt_at = now() + interval '1 minute' WHERE id = $1", c.id)
		return err
	}
	base := strings.TrimRight(e.EdgePublicURL, "/")
	insecure, _ := row.options["allow_insecure_target"].(bool)
	switch {
	case base == "":
		return e.failCheck(ctx, c.id, "failed", "edge.public_url is not configured")
	case !insecure && !strings.HasPrefix(base, "https://"):
		return e.failCheck(ctx, c.id, "failed", "third-party detection needs a public https edge.public_url")
	}
	u, err := e.loadUpstream(ctx, c.upstreamID)
	if err != nil {
		return err
	}
	if _, err := e.probeKey(ctx, u); err != nil {
		return fmt.Errorf("probe key: %w", err)
	}
	var enc []byte
	if err := e.DB.QueryRow(ctx, "SELECT key_enc FROM system_keys WHERE sub2api_group_id = $1", *u.probeGroupID).Scan(&enc); err != nil {
		return err
	}
	// 临时令牌只路由到该上游的探测分组，截止时间后 Edge 拒绝
	token := "tm-tp-" + randx.String(40)
	hash := edgecfg.HashKey(token)
	deadline := c.deadline
	if err := e.Edge.Put(ctx, hash, edgecfg.KeyConfig{Status: "active", ExpiresAt: &deadline, Hops: []edgecfg.Hop{{
		Label: fmt.Sprintf("probe:%d", u.id), InternalKeyEnc: enc, KeyAAD: fmt.Sprintf("system-key:%d", *u.probeGroupID),
	}}}); err != nil {
		return fmt.Errorf("publish edge token: %w", err)
	}
	if _, err := e.DB.Exec(ctx, "UPDATE third_party_checks SET token_hash = $2, token_revoked = false WHERE id = $1", c.id, hash); err != nil {
		return err
	}
	// 留出 Edge 收到配置变更通知的时间，避免检测站的第一批请求被当成无效 Key
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Second):
	}
	extID, err := prov.Submit(ctx, thirdparty.Target{BaseURL: base + "/v1", APIKey: token, Model: c.model, ClaimedModel: c.model, Format: c.format})
	if err != nil {
		_ = e.Edge.Delete(ctx, hash)
		var rl *thirdparty.RateLimitedError
		if errors.As(err, &rl) {
			_, err := e.DB.Exec(ctx, `UPDATE third_party_checks SET token_revoked = true, next_attempt_at = now() + make_interval(secs => $2)
				WHERE id = $1`, c.id, rl.RetryAfter.Seconds())
			return err
		}
		attempts := c.attempts + 1
		if attempts >= 3 {
			return errors.Join(err, e.failCheck(ctx, c.id, "failed", err.Error()))
		}
		_, uerr := e.DB.Exec(ctx, `UPDATE third_party_checks SET token_revoked = true, attempts = $2, error = $3,
			next_attempt_at = now() + make_interval(mins => $2) WHERE id = $1`, c.id, attempts, truncateStr(err.Error(), 500))
		return errors.Join(err, uerr)
	}
	_, err = e.DB.Exec(ctx, `UPDATE third_party_checks SET status = 'running', external_id = $2, submitted_at = now(), attempts = 0, error = NULL,
		next_attempt_at = now() + interval '30 seconds' WHERE id = $1`, c.id, extID)
	return err
}

func (e *Engine) pollCheck(ctx context.Context, c tpCheckRow) error {
	prov, _, err := e.provider(ctx, c.provider)
	if err != nil {
		return err
	}
	if prov == nil || c.externalID == nil {
		return e.failCheck(ctx, c.id, "failed", "provider is not available")
	}
	res, err := prov.Poll(ctx, *c.externalID)
	switch {
	case errors.Is(err, thirdparty.ErrRunNotFound):
		return e.failCheck(ctx, c.id, "failed", "the run is no longer available at the provider")
	case err != nil:
		attempts := c.attempts + 1
		if attempts >= 30 {
			return errors.Join(err, e.failCheck(ctx, c.id, "failed", err.Error()))
		}
		_, uerr := e.DB.Exec(ctx, `UPDATE third_party_checks SET attempts = $2, error = $3, next_attempt_at = now() + interval '30 seconds'
			WHERE id = $1`, c.id, attempts, truncateStr(err.Error(), 500))
		return errors.Join(err, uerr)
	case res.Status == "running":
		_, err := e.DB.Exec(ctx, "UPDATE third_party_checks SET next_attempt_at = now() + interval '20 seconds' WHERE id = $1", c.id)
		return err
	}
	status, verdict := "completed", c.config.VerdictFor(res.Outcome)
	if res.Status == "failed" {
		status, verdict = "failed", "inconclusive"
	}
	var report *string
	if res.ReportURL != "" {
		report = &res.ReportURL
	}
	_, err = e.DB.Exec(ctx, `UPDATE third_party_checks SET status = $2, outcome = $3, verdict = $4, summary = $5, report_url = $6,
		error = NULLIF($7, ''), finished_at = now() WHERE id = $1`, c.id, status, res.Outcome, verdict, mustJSON(res.Summary), report, res.Error)
	return err
}

// revokeTokens 撤销已结束检测的 Edge 临时令牌。
func (e *Engine) revokeTokens(ctx context.Context) error {
	rows, err := e.DB.Query(ctx, `SELECT id, token_hash FROM third_party_checks
		WHERE status NOT IN ('pending', 'running') AND token_hash IS NOT NULL AND NOT token_revoked`)
	if err != nil {
		return err
	}
	type tok struct {
		id   int64
		hash string
	}
	toks, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (tok, error) {
		var t tok
		err := row.Scan(&t.id, &t.hash)
		return t, err
	})
	if err != nil {
		return err
	}
	for _, t := range toks {
		if err := e.Edge.Delete(ctx, t.hash); err != nil {
			return err
		}
		if _, err := e.DB.Exec(ctx, "UPDATE third_party_checks SET token_revoked = true WHERE id = $1", t.id); err != nil {
			return err
		}
	}
	return nil
}

// finishThirdParty 对第三方检测全部结束的检测记录合并结论并处置。
func (e *Engine) finishThirdParty(ctx context.Context) error {
	rows, err := e.DB.Query(ctx, `SELECT r.id, r.upstream_id, r.trigger, r.model, r.summary FROM probe_runs r
		WHERE r.status = 'third_party' AND NOT EXISTS (
			SELECT 1 FROM third_party_checks c WHERE c.run_id = r.id AND c.status IN ('pending', 'running'))
		ORDER BY r.id LIMIT 50`)
	if err != nil {
		return err
	}
	type waiting struct {
		run     runRow
		summary []byte
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (waiting, error) {
		var w waiting
		err := row.Scan(&w.run.id, &w.run.upstreamID, &w.run.trigger, &w.run.model, &w.summary)
		return w, err
	})
	if err != nil {
		return err
	}
	for _, w := range list {
		if err := e.finishRun(ctx, w.run, w.summary); err != nil {
			e.Log.Error("finish third-party run", "run_id", w.run.id, "err", err)
			_, _ = e.DB.Exec(ctx, `UPDATE probe_runs SET status = 'failed', finished_at = now(), summary = $2 WHERE id = $1`,
				w.run.id, mustJSON(map[string]string{"error": err.Error()}))
		}
	}
	return nil
}

func (e *Engine) finishRun(ctx context.Context, run runRow, summary []byte) error {
	var s struct {
		Internal Assessment `json:"internal"`
	}
	if err := json.Unmarshal(summary, &s); err != nil {
		return err
	}
	rows, err := e.DB.Query(ctx, `SELECT provider, status, COALESCE(outcome, ''), COALESCE(verdict, 'inconclusive'), model,
			COALESCE(report_url, ''), COALESCE(error, ''), summary
		FROM third_party_checks WHERE run_id = $1 ORDER BY id`, run.id)
	if err != nil {
		return err
	}
	results, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (tpResult, error) {
		var r tpResult
		var sum []byte
		if err := row.Scan(&r.Provider, &r.Status, &r.Outcome, &r.Verdict, &r.Model, &r.ReportURL, &r.Error, &sum); err != nil {
			return r, err
		}
		if len(sum) > 0 {
			_ = json.Unmarshal(sum, &r.Summary)
		}
		return r, nil
	})
	if err != nil {
		return err
	}
	u, err := e.loadUpstream(ctx, run.upstreamID)
	if err != nil {
		return err
	}
	p, err := EffectivePolicy(ctx, e.DB, u.id, u.platform, u.authType)
	if err != nil {
		return err
	}
	return e.conclude(ctx, run, u, p, s.Internal, results)
}
