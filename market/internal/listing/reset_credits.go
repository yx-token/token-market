package listing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/yx-token/idlicat/market/internal/platform/httpx"
	"github.com/yx-token/idlicat/market/internal/sub2api/adminclient"
	"github.com/yx-token/idlicat/market/internal/sub2api/ops"
	"github.com/yx-token/idlicat/market/internal/sub2api/opshandlers"
	"github.com/jackc/pgx/v5"
)

// 重置卡：部分订阅（Codex、Claude）有官方发放的重置卡，用一张可以立即恢复额度窗口。
// 卖家可以手动用卡；Codex 账号还可以授权平台在窗口用量达到阈值时自动用卡，由 sub2api 的自动用卡任务执行，
// 本系统负责（经调度器）下发配置，并从 sub2api 的审计日志同步用卡记录。

// SupportsResetCredits 判断上游能否使用重置卡。sub2api 只支持 OpenAI OAuth 与 Claude OAuth（Setup Token 不行）。
func SupportsResetCredits(platform, authType string) bool {
	return authType == "oauth" && (platform == "openai" || platform == "anthropic")
}

// SupportsAutoReset 判断上游能否授权平台自动用卡（sub2api 只实现了 Codex 的自动用卡）。
func SupportsAutoReset(platform, authType string) bool {
	return platform == "openai" && authType == "oauth"
}

// PauseLine 返回平台暂停调度的窗口用量百分比：100 − 自留比例；不支持自留比例的厂商整号出售，为 100。
func PauseLine(platform string, selfReserve int) int {
	if SupportsSelfReserve(platform) {
		return 100 - selfReserve
	}
	return 100
}

// AutoResetThresholds 返回实际生效的自动用卡阈值（百分比）：卖家设定的值，未设定时跟随暂停线，且不超过暂停线。
func AutoResetThresholds(platform string, selfReserve int, p5h, p7d *int) (int, int) {
	line := PauseLine(platform, selfReserve)
	pick := func(p *int) int {
		if p == nil || *p > line {
			return line
		}
		return *p
	}
	return pick(p5h), pick(p7d)
}

// 用卡记录状态
const (
	ResetPending  = "pending"
	ResetSuccess  = "success"
	ResetNoCredit = "no_credit"
	ResetFailed   = "failed"
	ResetUnknown  = "unknown" // 请求已发出但结果不确定（超时、网络中断），卡可能已经用掉
)

type ResetCreditEvent struct {
	ID         int64          `json:"id"`
	Trigger    string         `json:"trigger"` // manual / auto
	Status     string         `json:"status"`
	Windows    []string       `json:"windows"`
	ErrorCode  *string        `json:"error_code,omitempty"`
	Detail     map[string]any `json:"detail,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	FinishedAt *time.Time     `json:"finished_at,omitempty"`
}

const resetEventColumns = `id, trigger, status, windows, error_code, detail, created_at, finished_at`

func scanResetEvent(row pgx.Row) (*ResetCreditEvent, error) {
	var e ResetCreditEvent
	var detail []byte
	if err := row.Scan(&e.ID, &e.Trigger, &e.Status, &e.Windows, &e.ErrorCode, &detail, &e.CreatedAt, &e.FinishedAt); err != nil {
		return nil, err
	}
	if len(detail) > 0 {
		if err := json.Unmarshal(detail, &e.Detail); err != nil {
			return nil, err
		}
	}
	if e.Windows == nil {
		e.Windows = []string{}
	}
	return &e, nil
}

// ResetCreditEvents 返回上游最近的用卡记录。
func ResetCreditEvents(ctx context.Context, db DB, upstreamID int64, limit int) ([]ResetCreditEvent, error) {
	rows, err := db.Query(ctx, "SELECT "+resetEventColumns+" FROM reset_credit_events WHERE upstream_id = $1 ORDER BY id DESC LIMIT $2", upstreamID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ResetCreditEvent{}
	for rows.Next() {
		e, err := scanResetEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

// ---- 查询 ----

type ResetCreditItem struct {
	Label      string     `json:"label,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	ResetsLeft int        `json:"resets_left"`
	Clears     []string   `json:"clears,omitempty"` // Claude：用卡会恢复的窗口
	Redeemable bool       `json:"redeemable"`
	Blocking   []string   `json:"blocking,omitempty"` // Claude：暂时不能用的原因
}

type AutoResetView struct {
	Enabled      bool                              `json:"enabled"` // 卖家已授权
	Active       bool                              `json:"active"`  // sub2api 中已开启：授权且挂单正在面向买家时才开启
	Threshold5h  *int                              `json:"threshold_5h_percent,omitempty"`
	Threshold7d  *int                              `json:"threshold_7d_percent,omitempty"`
	Effective5h  int                               `json:"effective_5h_percent"`
	Effective7d  int                               `json:"effective_7d_percent"`
	PauseLine    int                               `json:"pause_line_percent"`
	AuthorizedAt *time.Time                        `json:"authorized_at,omitempty"`
	State        *adminclient.OpenAIAutoResetState `json:"state,omitempty"`
}

// ResetCredits 是卖家看到的重置卡情况。
type ResetCredits struct {
	Platform       string             `json:"platform"`
	Queried        bool               `json:"queried"` // 是否有官方数据（Claude 需要点"查询"才向官方查询）
	Source         string             `json:"source,omitempty"`
	AvailableCount int                `json:"available_count"`
	Credits        []ResetCreditItem  `json:"credits"`
	CooldownUntil  *time.Time         `json:"cooldown_until,omitempty"`
	FetchedAt      *time.Time         `json:"fetched_at,omitempty"`
	Error          string             `json:"error,omitempty"`
	Auto           *AutoResetView     `json:"auto,omitempty"`
	Events         []ResetCreditEvent `json:"events"`
}

type resetCacheEntry struct {
	rc *ResetCredits
	at time.Time
}

// resetCache 缓存每个上游最近一次向官方查询的结果。Claude 的查询 sub2api 不缓存、每次都访问官方。
var resetCache = struct {
	sync.Mutex
	m map[int64]resetCacheEntry
}{m: map[int64]resetCacheEntry{}}

const (
	resetRefreshGap = 30 * time.Second // 向官方查询的最小间隔
	resetCacheTTL   = 10 * time.Minute // Claude 查询结果的展示期限
)

func forgetResetCredits(upstreamID int64) {
	resetCache.Lock()
	delete(resetCache.m, upstreamID)
	resetCache.Unlock()
	quotaCache.Lock()
	delete(quotaCache.m, upstreamID)
	quotaCache.Unlock()
}

// ResetCredits 返回上游的重置卡。Codex 默认读 sub2api 缓存的快照，Claude 默认返回最近一次查询结果；
// refresh 为真时向官方查询（30 秒内复用上次结果）。
func (s *Service) ResetCredits(ctx context.Context, sellerID, upstreamID int64, refresh bool) (*ResetCredits, error) {
	u, err := s.resetUpstream(ctx, sellerID, upstreamID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	resetCache.Lock()
	cached, ok := resetCache.m[u.ID]
	resetCache.Unlock()
	var rc *ResetCredits
	var account *adminclient.Account
	switch {
	case ok && now.Sub(cached.at) < resetRefreshGap, ok && !refresh && u.Platform == "anthropic" && now.Sub(cached.at) < resetCacheTTL:
		cp := *cached.rc
		rc = &cp
	case refresh:
		rc = s.queryResetCredits(ctx, u)
		if rc.Error == "" {
			resetCache.Lock()
			resetCache.m[u.ID] = resetCacheEntry{rc: rc, at: now}
			resetCache.Unlock()
		}
	default:
		rc = &ResetCredits{Platform: u.Platform, Credits: []ResetCreditItem{}}
	}
	if u.Platform == "openai" {
		// Codex：没有新查询时用 sub2api 缓存在账号 extra 中的快照；自动用卡状态也在 extra 中
		if account, err = s.Admin.GetAccount(ctx, *u.Sub2APIAccountID); err != nil {
			s.Log.Warn("reset credits: get account", "upstream_id", u.ID, "err", err)
		} else if !rc.Queried {
			if snap := adminclient.ParseOpenAIResetCredits(account.Extra); snap != nil {
				fillOpenAICredits(rc, snap)
				rc.Source = "snapshot"
			}
		}
	}
	if SupportsAutoReset(u.Platform, u.AuthType) {
		e5, e7 := AutoResetThresholds(u.Platform, u.SelfReservePercent, u.AutoReset5hPercent, u.AutoReset7dPercent)
		rc.Auto = &AutoResetView{
			Enabled: u.AutoResetEnabled, Threshold5h: u.AutoReset5hPercent, Threshold7d: u.AutoReset7dPercent,
			Effective5h: e5, Effective7d: e7, PauseLine: PauseLine(u.Platform, u.SelfReservePercent), AuthorizedAt: u.AutoResetAuthorizedAt,
		}
		if account != nil {
			rc.Auto.Active = account.Extra["auto_reset_credit_enabled"] == true
			rc.Auto.State = adminclient.ParseOpenAIAutoResetState(account.Extra)
		}
	}
	if rc.Events, err = ResetCreditEvents(ctx, s.DB, u.ID, 20); err != nil {
		return nil, err
	}
	return rc, nil
}

// resetUpstream 返回可以使用重置卡的卖家上游。
func (s *Service) resetUpstream(ctx context.Context, sellerID, upstreamID int64) (*Upstream, error) {
	u, err := GetSellerUpstream(ctx, s.DB, sellerID, upstreamID)
	if err != nil {
		return nil, err
	}
	if !SupportsResetCredits(u.Platform, u.AuthType) {
		return nil, httpx.BadRequest("UNSUPPORTED", "reset credits are only available for Codex and Claude OAuth accounts")
	}
	if u.Sub2APIAccountID == nil || u.Status == StatusRemoving || u.Status == StatusRemoved {
		return nil, ErrBadState
	}
	return u, nil
}

// queryResetCredits 向官方查询重置卡。失败时返回带 Error 的结果，不返回错误，便于前端照常展示其他信息。
func (s *Service) queryResetCredits(ctx context.Context, u *Upstream) *ResetCredits {
	now := s.now()
	rc := &ResetCredits{Platform: u.Platform, Credits: []ResetCreditItem{}, Source: "live", FetchedAt: &now}
	switch u.Platform {
	case "openai":
		q, err := s.Admin.RefreshOpenAIQuota(ctx, *u.Sub2APIAccountID)
		if err != nil {
			s.Log.Warn("reset credits: openai quota", "upstream_id", u.ID, "err", err)
			rc.Error = "暂时无法查询官方重置卡"
			return rc
		}
		if q.RateLimitResetCredits == nil {
			rc.Error = "官方没有返回重置卡信息"
			return rc
		}
		fillOpenAICredits(rc, q.RateLimitResetCredits)
	case "anthropic":
		c, err := s.Admin.ClaudeResetCredits(ctx, *u.Sub2APIAccountID)
		if err != nil {
			s.Log.Warn("reset credits: claude", "upstream_id", u.ID, "err", err)
			rc.Error = "暂时无法查询官方重置卡"
			if apiReason(err) == "CLAUDE_RESET_PROFILE_SCOPE_REQUIRED" {
				rc.Error = "授权缺少读取账号资料的权限，请重新授权后再查询"
			}
			return rc
		}
		rc.Queried, rc.AvailableCount, rc.CooldownUntil = true, c.AvailableCount, c.CooldownUntil
		for _, g := range c.Credits {
			rc.Credits = append(rc.Credits, ResetCreditItem{Label: g.Label, ExpiresAt: g.ExpiresAt, ResetsLeft: g.ResetsLeft,
				Clears: g.Clears, Redeemable: g.Redeemable, Blocking: g.Blocking})
		}
	}
	return rc
}

func fillOpenAICredits(rc *ResetCredits, c *adminclient.OpenAIResetCredits) {
	rc.Queried, rc.AvailableCount = true, c.AvailableCount
	rc.Credits = rc.Credits[:0]
	for _, it := range c.Credits {
		item := ResetCreditItem{ResetsLeft: 1, Redeemable: true}
		if t, err := time.Parse(time.RFC3339, it.ExpiresAt); err == nil {
			item.ExpiresAt = &t
		}
		rc.Credits = append(rc.Credits, item)
	}
}

// ---- 手动用卡 ----

type RedeemInput struct {
	RequestID         string `json:"request_id"`         // 卖家每次确认时生成；网络失败重试沿用同一个，返回首次结果
	ExpectedAvailable *int   `json:"expected_available"` // 卖家确认时看到的可用张数
}

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

const (
	redeemTimeout    = 100 * time.Second
	pendingStaleTime = 5 * time.Minute
	// Claude 用卡后 sub2api 的用量快照仍是旧值；它的主动查询有 3 分钟缓存，等缓存过期后再查询一次并恢复调度
	claudeResyncDelay = 4 * time.Minute
)

// RedeemResetCredit 手动使用一张重置卡。sub2api 的 Codex 用卡接口不幂等，为避免多花卡：
// 同一上游同时只允许一次用卡；用卡前向官方确认可用张数，与卖家确认时看到的一致才继续；
// 同一请求号重试直接返回首次结果。
func (s *Service) RedeemResetCredit(ctx context.Context, sellerID, upstreamID int64, in RedeemInput) (*ResetCreditEvent, error) {
	u, err := s.resetUpstream(ctx, sellerID, upstreamID)
	if err != nil {
		return nil, err
	}
	if !requestIDPattern.MatchString(in.RequestID) {
		return nil, httpx.BadRequest("INVALID_REQUEST_ID", "request_id must be 8-64 letters, digits, - or _")
	}
	if in.ExpectedAvailable == nil {
		return nil, httpx.BadRequest("EXPECTED_COUNT_REQUIRED", "expected_available is required")
	}
	if e, err := scanResetEvent(s.DB.QueryRow(ctx, "SELECT "+resetEventColumns+` FROM reset_credit_events
		WHERE upstream_id = $1 AND request_id = $2`, u.ID, in.RequestID)); err == nil {
		if e.Status == ResetPending {
			return nil, errResetInProgress
		}
		return e, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	// 进程中断遗留的进行中记录视为结果未知，避免永久占住
	if _, err := s.DB.Exec(ctx, `UPDATE reset_credit_events SET status = 'unknown', error_code = 'INTERRUPTED', finished_at = now()
		WHERE upstream_id = $1 AND status = 'pending' AND created_at < $2`, u.ID, s.now().Add(-pendingStaleTime)); err != nil {
		return nil, err
	}
	var eventID int64
	err = s.DB.QueryRow(ctx, `INSERT INTO reset_credit_events (upstream_id, seller_id, trigger, request_id, status)
		VALUES ($1, $2, 'manual', $3, 'pending') RETURNING id`, u.ID, u.SellerID, in.RequestID).Scan(&eventID)
	if isUniqueViolation(err) {
		return nil, errResetInProgress
	}
	if err != nil {
		return nil, err
	}
	// 还没动卡之前失败：删除占位记录，不留用卡记录
	abort := func(cause error) (*ResetCreditEvent, error) {
		if _, err := s.DB.Exec(context.WithoutCancel(ctx), "DELETE FROM reset_credit_events WHERE id = $1 AND status = 'pending'", eventID); err != nil {
			s.Log.Error("reset credits: drop pending event", "event_id", eventID, "err", err)
		}
		return nil, cause
	}
	available, err := s.liveAvailable(ctx, u)
	if err != nil {
		return abort(err)
	}
	if available == 0 {
		return abort(httpx.Conflict("NO_RESET_CREDIT", "no reset credit is available right now"))
	}
	if available != *in.ExpectedAvailable {
		return abort(httpx.Conflict("RESET_CREDITS_CHANGED", fmt.Sprintf("available reset credits changed to %d, please confirm again", available)))
	}

	// 卡一旦发出请求就可能被消耗：与卖家的连接解耦，保证结果落库
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), redeemTimeout)
	defer cancel()
	var out redeemOutcome
	switch u.Platform {
	case "openai":
		out = s.redeemOpenAI(cctx, u)
	case "anthropic":
		out = s.redeemClaude(cctx, u, eventID)
	}
	out.detail["available_before"] = available
	detail, _ := json.Marshal(out.detail)
	if out.windows == nil {
		out.windows = []string{}
	}
	e, err := scanResetEvent(s.DB.QueryRow(cctx, `UPDATE reset_credit_events SET status = $2, windows = $3, error_code = NULLIF($4, ''),
		detail = $5, finished_at = now() WHERE id = $1 RETURNING `+resetEventColumns, eventID, out.status, out.windows, out.errorCode, detail))
	if err != nil {
		return nil, err
	}
	auditDetail, _ := json.Marshal(map[string]any{"event_id": e.ID, "platform": u.Platform, "status": e.Status, "error_code": out.errorCode})
	if _, err := s.DB.Exec(cctx, `INSERT INTO audit_logs (actor_type, actor_id, action, target, detail) VALUES ('seller', $1, 'upstream.reset_credit', $2, $3)`,
		u.SellerID, fmt.Sprintf("upstream:%d", u.ID), auditDetail); err != nil {
		s.Log.Warn("reset credits: audit", "upstream_id", u.ID, "err", err)
	}
	forgetResetCredits(u.ID)
	return e, nil
}

var errResetInProgress = httpx.Conflict("RESET_IN_PROGRESS", "another reset is in progress for this upstream")

type redeemOutcome struct {
	status    string
	windows   []string
	errorCode string
	detail    map[string]any
}

// liveAvailable 向官方查询当前可用的重置卡张数。
func (s *Service) liveAvailable(ctx context.Context, u *Upstream) (int, error) {
	rc := s.queryResetCredits(ctx, u)
	if rc.Error != "" {
		return 0, httpx.Unavailable("RESET_CREDITS_QUERY_FAILED", rc.Error)
	}
	resetCache.Lock()
	resetCache.m[u.ID] = resetCacheEntry{rc: rc, at: s.now()}
	resetCache.Unlock()
	return rc.AvailableCount, nil
}

// classifyRedeemError：sub2api 明确拒绝（4xx）时卡没有被用掉；超时、网络错误与 5xx 时结果不确定。
func classifyRedeemError(err error) (status, code string) {
	var ae *adminclient.APIError
	if !errors.As(err, &ae) {
		return ResetUnknown, "REQUEST_FAILED"
	}
	code = firstNonEmpty(apiReason(err), fmt.Sprintf("HTTP_%d", ae.HTTPStatus))
	if ae.HTTPStatus >= 400 && ae.HTTPStatus < 500 {
		return ResetFailed, code
	}
	return ResetUnknown, code
}

// apiReason 返回 sub2api 错误的原因码（reason，没有时用字符串形式的 code）。
func apiReason(err error) string {
	var ae *adminclient.APIError
	if !errors.As(err, &ae) {
		return ""
	}
	return firstNonEmpty(ae.Reason, ae.ErrorCode)
}

func (s *Service) redeemOpenAI(ctx context.Context, u *Upstream) redeemOutcome {
	out := redeemOutcome{detail: map[string]any{}}
	res, err := s.Admin.ResetOpenAIQuota(ctx, *u.Sub2APIAccountID)
	if err != nil {
		s.Log.Warn("reset credits: openai reset", "upstream_id", u.ID, "err", err)
		out.status, out.errorCode = classifyRedeemError(err)
		return out
	}
	out.detail["code"], out.detail["windows_reset"] = res.Code, res.WindowsReset
	if strings.EqualFold(strings.TrimSpace(res.Code), "no_credit") {
		out.status, out.errorCode = ResetNoCredit, "NO_RESET_CREDIT"
		return out
	}
	out.status = ResetSuccess
	if res.WarningCode != "" {
		// 卡已用掉，但 sub2api 没能完成恢复账号状态或刷新快照：稍后由出站队列补做一次
		out.detail["warning_code"] = res.WarningCode
		s.enqueueResync(ctx, u, fmt.Sprintf("reset-resync-%d-%d", u.ID, s.now().UnixNano()), time.Time{})
	}
	return out
}

func (s *Service) redeemClaude(ctx context.Context, u *Upstream, eventID int64) redeemOutcome {
	out := redeemOutcome{detail: map[string]any{}}
	res, err := s.Admin.RedeemClaudeResetCredit(ctx, *u.Sub2APIAccountID, fmt.Sprintf("tm-reset-%d", eventID))
	if err != nil {
		s.Log.Warn("reset credits: claude redeem", "upstream_id", u.ID, "err", err)
		out.status, out.errorCode = classifyRedeemError(err)
		return out
	}
	out.detail["outcome"], out.windows = res.Outcome, res.Cleared
	if res.Reason != "" {
		out.detail["reason"] = res.Reason
	}
	switch res.Outcome {
	case "reset":
		out.status = ResetSuccess
		// sub2api 兑换后不恢复账号：先清除限流与临时暂停，等它的用量缓存过期后再刷新快照、恢复一次
		if err := s.Admin.RecoverAccountState(ctx, *u.Sub2APIAccountID); err != nil {
			s.Log.Warn("reset credits: recover account", "upstream_id", u.ID, "err", err)
		}
		s.enqueueResync(ctx, u, fmt.Sprintf("reset-resync-%d", eventID), s.now().Add(claudeResyncDelay))
	case "unknown":
		out.status, out.errorCode = ResetUnknown, strings.ToUpper(firstNonEmpty(res.Reason, "unknown"))
	default:
		out.status, out.errorCode = ResetFailed, strings.ToUpper(firstNonEmpty(res.Reason, res.Outcome))
	}
	return out
}

// enqueueResync 排队"重新查询用量并恢复账号状态"。用独立的 target，延时操作不会阻塞调度器对该账号的调整。
func (s *Service) enqueueResync(ctx context.Context, u *Upstream, key string, notBefore time.Time) {
	if _, err := ops.Enqueue(ctx, s.DB, ops.NewOp{
		Type: opshandlers.TypeAccountResyncRecover, Target: fmt.Sprintf("account-resync:%d", *u.Sub2APIAccountID),
		Payload: opshandlers.AccountResyncRecoverPayload{AccountID: *u.Sub2APIAccountID}, IdempotencyKey: key, NotBefore: notBefore, MaxAttempts: 3,
	}); err != nil {
		s.Log.Error("reset credits: enqueue resync", "upstream_id", u.ID, "err", err)
	}
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// ---- 授权自动用卡 ----

type AutoResetInput struct {
	Enabled     bool `json:"enabled"`
	Threshold5h *int `json:"threshold_5h_percent"` // 空表示跟随暂停线
	Threshold7d *int `json:"threshold_7d_percent"`
}

// SetAutoReset 授权或取消平台自动用卡（仅 Codex）。窗口用量达到阈值时平台暂停售卖并尝试用卡，用卡成功后恢复；
// 没有可用的卡时保持暂停直到窗口自然重置。阈值不能高于暂停线（100 − 自留比例）。
func (s *Service) SetAutoReset(ctx context.Context, sellerID, upstreamID int64, in AutoResetInput) (*Upstream, error) {
	u, err := s.resetUpstream(ctx, sellerID, upstreamID)
	if err != nil {
		return nil, err
	}
	if !SupportsAutoReset(u.Platform, u.AuthType) {
		return nil, httpx.BadRequest("UNSUPPORTED", "automatic reset credits are only available for Codex OAuth accounts")
	}
	line := PauseLine(u.Platform, u.SelfReservePercent)
	for _, p := range []*int{in.Threshold5h, in.Threshold7d} {
		if p != nil && (*p < 1 || *p > line) {
			return nil, httpx.BadRequest("INVALID_AUTO_RESET_THRESHOLD", fmt.Sprintf("threshold must be between 1 and %d (the pause line)", line))
		}
	}
	if _, err := s.DB.Exec(ctx, `UPDATE upstreams SET auto_reset_enabled = $2, auto_reset_5h_percent = $3, auto_reset_7d_percent = $4,
		auto_reset_authorized_at = CASE WHEN $2 AND NOT auto_reset_enabled THEN now() ELSE auto_reset_authorized_at END, updated_at = now()
		WHERE id = $1`, u.ID, in.Enabled, in.Threshold5h, in.Threshold7d); err != nil {
		return nil, err
	}
	detail, _ := json.Marshal(in)
	if _, err := s.DB.Exec(ctx, `INSERT INTO audit_logs (actor_type, actor_id, action, target, detail) VALUES ('seller', $1, 'upstream.auto_reset', $2, $3)`,
		u.SellerID, fmt.Sprintf("upstream:%d", u.ID), detail); err != nil {
		s.Log.Warn("auto reset: audit", "upstream_id", u.ID, "err", err)
	}
	s.kick(ctx)
	return GetUpstream(ctx, s.DB, u.ID)
}

// ---- 同步自动用卡记录 ----

// RecordAutoResetAudits 把 sub2api 自动用卡的审计记录写成用卡记录（按审计 ID 幂等）。
// accountUpstream 是 sub2api 账号 ID → 上游 ID。no_credit 只在发起兑换后官方才发现没卡时出现，同样记录。
func RecordAutoResetAudits(ctx context.Context, db DB, audits []AutoResetAuditInput, accountUpstream map[int64]int64) error {
	for _, a := range audits {
		upstreamID, ok := accountUpstream[a.AccountID]
		if !ok {
			continue
		}
		status := ResetFailed
		switch a.ResultCode {
		case "success":
			status = ResetSuccess
		case "no_credit":
			status = ResetNoCredit
		}
		var windows []string
		if a.TriggerWindow != "" {
			windows = strings.Split(a.TriggerWindow, "+")
		} else {
			windows = []string{}
		}
		detail, _ := json.Marshal(map[string]any{"result_code": a.ResultCode, "windows_reset": a.WindowsReset})
		if _, err := db.Exec(ctx, `INSERT INTO reset_credit_events (upstream_id, seller_id, trigger, request_id, status, windows, error_code, detail, created_at, finished_at)
			SELECT id, seller_id, 'auto', $2, $3, $4, NULLIF($5, ''), $6, $7, $7 FROM upstreams WHERE id = $1
			ON CONFLICT (upstream_id, request_id) DO NOTHING`,
			upstreamID, fmt.Sprintf("auto:%d", a.AuditID), status, windows, a.ErrorCode, detail, a.At); err != nil {
			return err
		}
	}
	return nil
}

// AutoResetAuditInput 是一条 sub2api 自动用卡审计记录中本系统关心的字段。
type AutoResetAuditInput struct {
	AuditID       int64
	At            time.Time
	AccountID     int64
	ResultCode    string // success / no_credit / failed / recovery_failed
	TriggerWindow string // 5h / 7d / 5h+7d
	WindowsReset  int
	ErrorCode     string
}
