package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/DueGin/token-market/internal/billing"
	"github.com/DueGin/token-market/internal/listing"
	"github.com/DueGin/token-market/internal/platform/httpx"
	"github.com/DueGin/token-market/internal/quality"
	"github.com/DueGin/token-market/internal/scheduling"
	"github.com/DueGin/token-market/internal/settings"
	"github.com/jackc/pgx/v5"
)

func adminID(r *http.Request) int64 { return httpx.MustUser(r).UserID }

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) error {
	st, err := s.settings(r.Context())
	if err != nil {
		return err
	}
	return ok(w, st)
}

func (s *Server) updateSettings(w http.ResponseWriter, r *http.Request) error {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return err
	}
	st, err := settings.Update(r.Context(), s.DB, raw)
	if err != nil {
		return httpx.BadRequest("INVALID_SETTINGS", err.Error())
	}
	s.audit(r, "settings.update", "settings", json.RawMessage(raw))
	if s.Payments != nil {
		s.Payments.Invalidate() // 支付通知地址可能变化
	}
	if s.Kicker != nil {
		s.Kicker.Kick(r.Context())
	}
	return ok(w, st)
}

func (s *Server) adminVendors(w http.ResponseWriter, r *http.Request) error {
	vs, err := listing.ListVendors(r.Context(), s.DB)
	if err != nil {
		return err
	}
	return ok(w, vs)
}

func (s *Server) updateVendor(w http.ResponseWriter, r *http.Request) error {
	var in listing.UpdateVendorInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	v, err := listing.UpdateVendor(r.Context(), s.DB, r.PathValue("platform"), in)
	if err != nil {
		return err
	}
	s.audit(r, "vendor.update", "vendor:"+v.Platform, in)
	if s.Kicker != nil {
		s.Kicker.Kick(r.Context())
	}
	return ok(w, v)
}

func (s *Server) listProxies(w http.ResponseWriter, r *http.Request) error {
	ps, err := s.Proxies.List(r.Context())
	if err != nil {
		return err
	}
	return ok(w, ps)
}

func (s *Server) createProxy(w http.ResponseWriter, r *http.Request) error {
	var in listing.CreateProxyInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	p, err := s.Proxies.Create(r.Context(), in)
	if err != nil {
		return err
	}
	s.audit(r, "proxy.create", fmt.Sprintf("proxy:%d", p.ID), map[string]any{"host": p.Host, "port": p.Port})
	return created(w, p)
}

func (s *Server) syncProxy(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	p, err := s.Proxies.Sync(r.Context(), id)
	if err != nil {
		return err
	}
	return ok(w, p)
}

func (s *Server) setProxyDisabled(disabled bool) httpx.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		id, err := httpx.PathID(r, "id")
		if err != nil {
			return err
		}
		p, err := s.Proxies.SetDisabled(r.Context(), id, disabled)
		if err != nil {
			return err
		}
		s.audit(r, "proxy.set_disabled", fmt.Sprintf("proxy:%d", id), map[string]bool{"disabled": disabled})
		return ok(w, p)
	}
}

func (s *Server) listPools(w http.ResponseWriter, r *http.Request) error {
	ps, err := scheduling.ListPools(r.Context(), s.DB, false)
	if err != nil {
		return err
	}
	return ok(w, ps)
}

func (s *Server) createPool(w http.ResponseWriter, r *http.Request) error {
	var in scheduling.PoolInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	p, err := s.Pools.Create(r.Context(), in)
	if err != nil {
		return err
	}
	s.audit(r, "pool.create", fmt.Sprintf("pool:%d", p.ID), in)
	if s.Kicker != nil {
		s.Kicker.Kick(r.Context())
	}
	return created(w, p)
}

func (s *Server) updatePool(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var in scheduling.PoolInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	p, err := s.Pools.Update(r.Context(), id, in)
	if err != nil {
		return err
	}
	s.audit(r, "pool.update", fmt.Sprintf("pool:%d", id), in)
	if s.Kicker != nil {
		s.Kicker.Kick(r.Context())
	}
	return ok(w, p)
}

func (s *Server) syncPool(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	p, err := s.Pools.Sync(r.Context(), id)
	if err != nil {
		return err
	}
	return ok(w, p)
}

func (s *Server) listPolicies(w http.ResponseWriter, r *http.Request) error {
	ps, err := quality.ListPolicies(r.Context(), s.DB)
	if err != nil {
		return err
	}
	return ok(w, ps)
}

func (s *Server) upsertPolicy(w http.ResponseWriter, r *http.Request) error {
	var in quality.PolicyInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	p, err := quality.UpsertPolicy(r.Context(), s.DB, r.PathValue("platform"), r.URL.Query().Get("auth_type"), in, adminID(r))
	if err != nil {
		return err
	}
	s.audit(r, "detection_policy.update", "policy:"+p.Platform+":"+p.AuthType, in)
	if s.Kicker != nil {
		s.Kicker.Kick(r.Context())
	}
	return ok(w, p)
}

func (s *Server) listDetectionProviders(w http.ResponseWriter, r *http.Request) error {
	ps, err := quality.ListProviders(r.Context(), s.DB)
	if err != nil {
		return err
	}
	return ok(w, ps)
}

func (s *Server) updateDetectionProvider(w http.ResponseWriter, r *http.Request) error {
	var in quality.ProviderInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	p, err := quality.UpsertProvider(r.Context(), s.DB, s.Quality.Keyring, r.PathValue("name"), in, adminID(r))
	if err != nil {
		return err
	}
	logged := in
	if logged.APIKey != nil {
		masked := "***"
		logged.APIKey = &masked
	}
	s.audit(r, "detection_provider.update", "detection_provider:"+p.Name, logged)
	return ok(w, p)
}

func (s *Server) listThirdPartyChecks(w http.ResponseWriter, r *http.Request) error {
	limit, offset := page(r)
	cs, err := quality.ListThirdPartyChecks(r.Context(), s.DB, queryInt64(r, "upstream_id"), limit, offset)
	if err != nil {
		return err
	}
	return ok(w, cs)
}

func (s *Server) setOverride(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var in quality.OverrideInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if err := quality.SetOverride(r.Context(), s.DB, id, in, adminID(r)); err != nil {
		return err
	}
	s.audit(r, "detection_override.set", fmt.Sprintf("upstream:%d", id), in)
	return ok(w, map[string]string{"status": "ok"})
}

func (s *Server) deleteOverride(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	if err := quality.DeleteOverride(r.Context(), s.DB, id); err != nil {
		return err
	}
	s.audit(r, "detection_override.delete", fmt.Sprintf("upstream:%d", id), nil)
	return ok(w, map[string]string{"status": "deleted"})
}

func (s *Server) adminProbe(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	runID, err := s.Quality.QueueRun(r.Context(), id, "admin")
	if err != nil {
		return err
	}
	s.audit(r, "probe.queue", fmt.Sprintf("upstream:%d", id), nil)
	return created(w, map[string]int64{"run_id": runID})
}

func (s *Server) listItems(w http.ResponseWriter, r *http.Request) error {
	items, err := quality.ListItems(r.Context(), s.DB, r.URL.Query().Get("status"))
	if err != nil {
		return err
	}
	return ok(w, items)
}

func (s *Server) createItem(w http.ResponseWriter, r *http.Request) error {
	var in quality.Item
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	it, err := quality.CreateItem(r.Context(), s.DB, in)
	if err != nil {
		return err
	}
	s.audit(r, "probe_item.create", fmt.Sprintf("probe_item:%d", it.ID), nil)
	return created(w, it)
}

func (s *Server) updateItem(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var in quality.Item
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	it, err := quality.UpdateItem(r.Context(), s.DB, id, in)
	if err != nil {
		return err
	}
	s.audit(r, "probe_item.update", fmt.Sprintf("probe_item:%d", id), nil)
	return ok(w, it)
}

func (s *Server) retireItem(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	tag, err := s.DB.Exec(r.Context(), "UPDATE probe_items SET status = 'retired', updated_at = now() WHERE id = $1 AND status <> 'retired'", id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.NotFound("ITEM_NOT_FOUND", "probe item not found or already retired")
	}
	s.audit(r, "probe_item.retire", fmt.Sprintf("probe_item:%d", id), nil)
	return ok(w, map[string]string{"status": "retired"})
}

func (s *Server) listReferences(w http.ResponseWriter, r *http.Request) error {
	rows, err := s.DB.Query(r.Context(), `SELECT id FROM upstreams WHERE is_reference AND status <> 'removed' ORDER BY platform, id`)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return err
	}
	out := make([]*listing.Upstream, 0, len(ids))
	for _, id := range ids {
		u, err := listing.GetUpstream(r.Context(), s.DB, id)
		if err != nil {
			return err
		}
		out = append(out, u)
	}
	return ok(w, out)
}

func (s *Server) deleteReference(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	u, err := listing.GetUpstream(r.Context(), s.DB, id)
	if err != nil {
		return err
	}
	if !u.IsReference {
		return httpx.BadRequest("NOT_REFERENCE", "upstream is not a reference account")
	}
	if err := s.Listings.AdminRemove(r.Context(), id); err != nil {
		return err
	}
	s.audit(r, "reference.remove", fmt.Sprintf("upstream:%d", id), nil)
	return ok(w, map[string]string{"status": "removing"})
}

// createReference 创建平台基线账号（属于当前管理员），之后通过卖家上游接口完成授权。
func (s *Server) createReference(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Platform string `json:"platform"`
		AuthType string `json:"auth_type"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	u, err := s.Listings.CreateUpstream(r.Context(), adminID(r), in.Platform, in.AuthType, true, nil, false)
	if err != nil {
		return err
	}
	s.audit(r, "reference.create", fmt.Sprintf("upstream:%d", u.ID), in)
	return created(w, map[string]any{"upstream": u, "next": "authorize via /api/seller/upstreams/{id}/oauth/start or /coding-key"})
}

func (s *Server) qualityOverview(w http.ResponseWriter, r *http.Request) error {
	o, err := quality.AdminOverview(r.Context(), s.DB)
	if err != nil {
		return err
	}
	return ok(w, o)
}

func (s *Server) listAppeals(w http.ResponseWriter, r *http.Request) error {
	limit, offset := page(r)
	as, err := quality.ListAppeals(r.Context(), s.DB, r.URL.Query().Get("status"), limit, offset)
	if err != nil {
		return err
	}
	return ok(w, as)
}

func (s *Server) resolveAppeal(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var in struct {
		Action string `json:"action"`
		Note   string `json:"note"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	a, err := s.Quality.ResolveAppeal(r.Context(), id, in.Action, in.Note)
	if err != nil {
		return err
	}
	s.audit(r, "appeal.resolve", fmt.Sprintf("appeal:%d", id), in)
	return ok(w, a)
}

func (s *Server) adminUpstreams(w http.ResponseWriter, r *http.Request) error {
	limit, offset := page(r)
	us, err := listing.ListUpstreams(r.Context(), s.DB, r.URL.Query().Get("status"), limit, offset)
	if err != nil {
		return err
	}
	return ok(w, us)
}

func (s *Server) suspendUpstream(suspend bool) httpx.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		id, err := httpx.PathID(r, "id")
		if err != nil {
			return err
		}
		var in struct {
			Reason string `json:"reason"`
		}
		if r.ContentLength > 0 {
			if err := httpx.Decode(r, &in); err != nil {
				return err
			}
		}
		u, err := s.Listings.AdminSetSuspended(r.Context(), id, suspend, in.Reason)
		if err != nil {
			return err
		}
		s.audit(r, "upstream.suspend", fmt.Sprintf("upstream:%d", id), map[string]any{"suspend": suspend, "reason": in.Reason})
		return ok(w, u)
	}
}

func (s *Server) adminRemoveUpstream(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	if err := s.Listings.AdminRemove(r.Context(), id); err != nil {
		return err
	}
	s.audit(r, "upstream.remove", fmt.Sprintf("upstream:%d", id), nil)
	return ok(w, map[string]string{"status": "removing"})
}

func (s *Server) pendingKYC(w http.ResponseWriter, r *http.Request) error {
	items, err := s.Accounts.PendingKYC(r.Context())
	if err != nil {
		return err
	}
	return ok(w, items)
}

func (s *Server) reviewKYC(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var in struct {
		Approve bool   `json:"approve"`
		Note    string `json:"note"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if err := s.Accounts.ReviewKYC(r.Context(), adminID(r), id, in.Approve, in.Note); err != nil {
		return err
	}
	s.audit(r, "kyc.review", fmt.Sprintf("user:%d", id), in)
	return ok(w, map[string]string{"status": "reviewed"})
}

func (s *Server) setUserStatus(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var in struct {
		Status string `json:"status"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if err := s.Accounts.SetStatus(r.Context(), id, in.Status); err != nil {
		return err
	}
	s.audit(r, "user.status", fmt.Sprintf("user:%d", id), in)
	return ok(w, map[string]string{"status": in.Status})
}

func (s *Server) adminRecharges(w http.ResponseWriter, r *http.Request) error {
	limit, offset := page(r)
	os, err := s.Funds.ListRecharges(r.Context(), 0, r.URL.Query().Get("status"), limit, offset)
	if err != nil {
		return err
	}
	return ok(w, os)
}

func (s *Server) confirmRecharge(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var in struct {
		ProviderRef string `json:"provider_ref"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	uid := adminID(r)
	o, err := s.Funds.ConfirmRecharge(r.Context(), id, in.ProviderRef, &uid)
	if err != nil {
		return err
	}
	s.audit(r, "recharge.confirm", fmt.Sprintf("recharge:%d", id), in)
	return ok(w, o)
}

func (s *Server) adminRefunds(w http.ResponseWriter, r *http.Request) error {
	limit, offset := page(r)
	rs, err := s.Funds.ListRefunds(r.Context(), 0, r.URL.Query().Get("status"), limit, offset)
	if err != nil {
		return err
	}
	return ok(w, rs)
}

func (s *Server) refundPaid(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var in struct {
		PayoutRef string `json:"payout_ref"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	ref, err := s.Funds.MarkRefundPaid(r.Context(), id, in.PayoutRef)
	if err != nil {
		return err
	}
	s.audit(r, "refund.paid", fmt.Sprintf("refund:%d", id), in)
	return ok(w, ref)
}

func (s *Server) adminWithdrawals(w http.ResponseWriter, r *http.Request) error {
	limit, offset := page(r)
	ws, err := s.Funds.ListWithdrawals(r.Context(), 0, r.URL.Query().Get("status"), true, limit, offset)
	if err != nil {
		return err
	}
	return ok(w, ws)
}

func (s *Server) reviewWithdrawal(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var in struct {
		Approve bool   `json:"approve"`
		Reason  string `json:"reason"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	wd, err := s.Funds.ReviewWithdrawal(r.Context(), adminID(r), id, in.Approve, in.Reason)
	if err != nil {
		return err
	}
	s.audit(r, "withdrawal.review", fmt.Sprintf("withdrawal:%d", id), in)
	return ok(w, wd)
}

func (s *Server) withdrawalPaid(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var in struct {
		PayoutRef string `json:"payout_ref"`
		Failed    bool   `json:"failed"`
		Reason    string `json:"reason"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	wd, err := s.Funds.MarkWithdrawalPaid(r.Context(), adminID(r), id, in.PayoutRef, in.Failed, in.Reason)
	if err != nil {
		return err
	}
	s.audit(r, "withdrawal.paid", fmt.Sprintf("withdrawal:%d", id), in)
	return ok(w, wd)
}

func (s *Server) listAnomalies(w http.ResponseWriter, r *http.Request) error {
	limit, offset := page(r)
	as, err := billing.ListAnomalies(r.Context(), s.DB, r.URL.Query().Get("status"), limit, offset)
	if err != nil {
		return err
	}
	return ok(w, as)
}

func (s *Server) resolveAnomaly(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var in struct {
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if err := billing.ResolveAnomaly(r.Context(), s.DB, id, in.Status, in.Note); err != nil {
		return err
	}
	s.audit(r, "anomaly.resolve", fmt.Sprintf("anomaly:%d", id), in)
	return ok(w, map[string]string{"status": in.Status})
}

func (s *Server) adjust(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		BuyerUserID int64   `json:"buyer_user_id"`
		Amount      float64 `json:"amount"`
		Reason      string  `json:"reason"`
		AnomalyID   *int64  `json:"anomaly_id"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if err := s.Funds.Adjust(r.Context(), adminID(r), in.BuyerUserID, in.Amount, in.Reason, in.AnomalyID); err != nil {
		return err
	}
	s.audit(r, "ledger.adjust", fmt.Sprintf("user:%d", in.BuyerUserID), in)
	return created(w, map[string]string{"status": "adjusted"})
}

func (s *Server) listRisk(w http.ResponseWriter, r *http.Request) error {
	limit, offset := page(r)
	es, err := s.Risk.List(r.Context(), r.URL.Query().Get("status"), limit, offset)
	if err != nil {
		return err
	}
	return ok(w, es)
}

func (s *Server) resolveRisk(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var in struct {
		Status string `json:"status"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if err := s.Risk.Resolve(r.Context(), adminID(r), id, in.Status); err != nil {
		return err
	}
	s.audit(r, "risk.resolve", fmt.Sprintf("risk:%d", id), in)
	return ok(w, map[string]string{"status": in.Status})
}

func (s *Server) platformLedger(w http.ResponseWriter, r *http.Request) error {
	l, err := billing.PlatformLedger(r.Context(), s.DB)
	if err != nil {
		return err
	}
	return ok(w, l)
}

type sub2apiStatus struct {
	Version     *string        `json:"sub2api_version"`
	Fingerprint *string        `json:"migration_fingerprint"`
	GuardState  string         `json:"guard_state"`
	Reason      *string        `json:"reason"`
	CheckedAt   *time.Time     `json:"checked_at"`
	Ops         map[string]int `json:"ops_by_status"`
	IngestedTo  int64          `json:"ingest_cursor"`
}

func (s *Server) sub2apiStatus(w http.ResponseWriter, r *http.Request) error {
	var st sub2apiStatus
	if err := s.DB.QueryRow(r.Context(), `SELECT sub2api_version, migration_fingerprint, guard_state, reason, checked_at FROM compat_state WHERE id = 1`).
		Scan(&st.Version, &st.Fingerprint, &st.GuardState, &st.Reason, &st.CheckedAt); err != nil {
		return err
	}
	if err := s.DB.QueryRow(r.Context(), "SELECT last_id FROM ingest_cursors WHERE source = 'usage_logs'").Scan(&st.IngestedTo); err != nil {
		return err
	}
	rows, err := s.DB.Query(r.Context(), `SELECT status, count(*) FROM sub2api_ops WHERE status <> 'done' GROUP BY status`)
	if err != nil {
		return err
	}
	defer rows.Close()
	st.Ops = map[string]int{}
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return err
		}
		st.Ops[status] = n
	}
	return ok(w, st)
}

func (s *Server) auditLogs(w http.ResponseWriter, r *http.Request) error {
	limit, offset := page(r)
	rows, err := s.DB.Query(r.Context(), `SELECT id, actor_type, actor_id, action, target, detail, created_at FROM audit_logs
		WHERE $1 = '' OR target = $1 ORDER BY id DESC LIMIT $2 OFFSET $3`, r.URL.Query().Get("target"), limit, offset)
	if err != nil {
		return err
	}
	defer rows.Close()
	type entry struct {
		ID        int64           `json:"id"`
		ActorType string          `json:"actor_type"`
		ActorID   *int64          `json:"actor_id"`
		Action    string          `json:"action"`
		Target    *string         `json:"target"`
		Detail    json.RawMessage `json:"detail"`
		CreatedAt time.Time       `json:"created_at"`
	}
	var out []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.ID, &e.ActorType, &e.ActorID, &e.Action, &e.Target, &e.Detail, &e.CreatedAt); err != nil {
			return err
		}
		out = append(out, e)
	}
	return ok(w, out)
}
