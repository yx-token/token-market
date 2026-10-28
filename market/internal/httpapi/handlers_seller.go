package httpapi

import (
	"net/http"
	"time"

	"github.com/yx-token/token-market/market/internal/billing"
	"github.com/yx-token/token-market/market/internal/listing"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
	"github.com/yx-token/token-market/market/internal/quality"
)

func (s *Server) createUpstream(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Platform        string `json:"platform"`
		AuthType        string `json:"auth_type"`
		ProxyID         *int64 `json:"proxy_id"`
		ProxyFromImport bool   `json:"proxy_from_import"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	u, err := s.Listings.CreateUpstream(r.Context(), httpx.MustUser(r).UserID, in.Platform, in.AuthType, false, in.ProxyID, in.ProxyFromImport)
	if err != nil {
		return err
	}
	return created(w, u)
}

func (s *Server) listUpstreams(w http.ResponseWriter, r *http.Request) error {
	us, err := listing.ListSellerUpstreams(r.Context(), s.DB, httpx.MustUser(r).UserID)
	if err != nil {
		return err
	}
	return ok(w, us)
}

type upstreamDetail struct {
	*listing.Upstream
	Listing  *listing.Listing `json:"listing,omitempty"`
	Quote    *listing.Quote   `json:"quote,omitempty"`
	Proxy    *listing.Proxy   `json:"proxy,omitempty"`
	Stats24h upstreamStats    `json:"stats_24h"`
	// 能否使用重置卡、能否授权平台自动用卡
	ResetCredits bool `json:"reset_credits_supported"`
	AutoReset    bool `json:"auto_reset_supported"`
	// 能否手动复测；检测正常时不允许，按钮置灰并说明原因
	ManualRetest *quality.RetestEligibility `json:"manual_retest,omitempty"`
}

// upstreamStats 是上游近 24 小时的服务情况（只含已入账的用量，有几分钟延迟）。
type upstreamStats struct {
	Requests    int      `json:"requests"`
	ProbeChecks int      `json:"probe_requests"`
	Payout      float64  `json:"payout"`
	TTFTp50Ms   *float64 `json:"ttft_p50_ms"`
	SuccessRate *float64 `json:"success_rate"` // 指定该挂单的 Edge 尝试成功率
}

func (s *Server) getUpstream(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	u, err := listing.GetSellerUpstream(r.Context(), s.DB, httpx.MustUser(r).UserID, id)
	if err != nil {
		return err
	}
	d := upstreamDetail{Upstream: u, ResetCredits: listing.SupportsResetCredits(u.Platform, u.AuthType),
		AutoReset: listing.SupportsAutoReset(u.Platform, u.AuthType)}
	if u.ProxyID != nil {
		if d.Proxy, err = s.Proxies.Get(r.Context(), *u.ProxyID); err != nil {
			return err
		}
	}
	if d.Listing, err = listing.OpenListing(r.Context(), s.DB, id); err != nil {
		return err
	}
	if d.Quote, err = listing.CurrentQuote(r.Context(), s.DB, id); err != nil {
		return err
	}
	if s.Quality != nil {
		eligibility, err := s.Quality.ManualRetestEligibility(r.Context(), u)
		if err != nil {
			return err
		}
		d.ManualRetest = &eligibility
	}
	if err := s.DB.QueryRow(r.Context(), `SELECT count(*) FILTER (WHERE channel IN ('pool', 'direct')), count(*) FILTER (WHERE channel = 'probe'),
			COALESCE(sum(payout) FILTER (WHERE channel IN ('pool', 'direct')), 0)::float8,
			percentile_cont(0.5) WITHIN GROUP (ORDER BY first_token_ms)
		FROM usage_records WHERE upstream_id = $1 AND created_at > now() - interval '24 hours'`, id).
		Scan(&d.Stats24h.Requests, &d.Stats24h.ProbeChecks, &d.Stats24h.Payout, &d.Stats24h.TTFTp50Ms); err != nil {
		return err
	}
	if d.Listing != nil {
		if err := s.DB.QueryRow(r.Context(), `SELECT (count(*) FILTER (WHERE end_reason IN ('completed', 'client_cancel')))::float8
				/ NULLIF(count(*) FILTER (WHERE end_reason <> 'skipped'), 0)
			FROM edge_attempts WHERE listing_id = $1 AND started_at > now() - interval '24 hours'`, d.Listing.ID).Scan(&d.Stats24h.SuccessRate); err != nil {
			return err
		}
	}
	return ok(w, d)
}

func (s *Server) startOAuth(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var opts listing.AuthorizationOptions
	if r.ContentLength != 0 {
		if err := decodeOAuth(r, &opts); err != nil {
			return err
		}
	}
	st, err := s.Listings.StartAuthorization(r.Context(), httpx.MustUser(r).UserID, id, opts)
	if err != nil {
		return err
	}
	return ok(w, st)
}

func (s *Server) completeOAuth(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var in oauthCompleteInput
	if err := decodeOAuth(r, &in); err != nil {
		return err
	}
	input, err := in.value()
	if err != nil {
		return err
	}
	u, err := s.Listings.CompleteAuthorization(r.Context(), httpx.MustUser(r).UserID, id, input)
	if err != nil {
		return err
	}
	return ok(w, u)
}

func (s *Server) reauthorize(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var opts listing.AuthorizationOptions
	if r.ContentLength != 0 {
		if err := decodeOAuth(r, &opts); err != nil {
			return err
		}
	}
	st, err := s.Listings.StartReauthorization(r.Context(), httpx.MustUser(r).UserID, id, opts)
	if err != nil {
		return err
	}
	next := "submit the code via /api/seller/upstreams/{id}/oauth/complete"
	if st.Flow == "device-code" || st.Flow == "init-poll" {
		next = "poll via /api/seller/upstreams/{id}/oauth/poll"
	}
	return ok(w, struct {
		*listing.AuthorizationStart
		Next string `json:"next"`
	}{st, next})
}

func (s *Server) submitCodingKey(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var in listing.CodingKeyInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	u, err := s.Listings.SubmitCodingKey(r.Context(), httpx.MustUser(r).UserID, id, in)
	if err != nil {
		return err
	}
	return ok(w, u)
}

func (s *Server) retryVerification(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	u, err := s.Listings.RetryVerification(r.Context(), httpx.MustUser(r).UserID, id)
	if err != nil {
		return err
	}
	return ok(w, u)
}

func (s *Server) upstreamModels(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	models, err := s.Listings.GetModels(r.Context(), httpx.MustUser(r).UserID, id)
	if err != nil {
		return err
	}
	return ok(w, models)
}

func (s *Server) refreshUpstreamModels(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	models, err := s.Listings.RefreshModels(r.Context(), httpx.MustUser(r).UserID, id)
	if err != nil {
		return err
	}
	return ok(w, models)
}

func (s *Server) setUpstreamModels(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var in struct {
		Models []string `json:"models"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	u, err := s.Listings.SetModels(r.Context(), httpx.MustUser(r).UserID, id, in.Models)
	if err != nil {
		return err
	}
	return ok(w, u)
}

func (s *Server) upstreamSettings(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var in struct {
		SelfReservePercent int `json:"self_reserve_percent"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	u, err := s.Listings.SetSelfReserve(r.Context(), httpx.MustUser(r).UserID, id, in.SelfReservePercent)
	if err != nil {
		return err
	}
	return ok(w, u)
}

func (s *Server) removeUpstream(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	if err := s.Listings.Remove(r.Context(), httpx.MustUser(r).UserID, id); err != nil {
		return err
	}
	return ok(w, map[string]string{"status": "removing"})
}

func (s *Server) createListing(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var in listing.CreateListingInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	l, q, err := s.Listings.CreateListing(r.Context(), httpx.MustUser(r).UserID, id, in)
	if err != nil {
		return err
	}
	return created(w, map[string]any{"listing": l, "quote": q})
}

func (s *Server) currentQuote(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	if _, err := listing.GetSellerUpstream(r.Context(), s.DB, httpx.MustUser(r).UserID, id); err != nil {
		return err
	}
	q, err := listing.CurrentQuote(r.Context(), s.DB, id)
	if err != nil {
		return err
	}
	return ok(w, q)
}

func (s *Server) sellerListings(w http.ResponseWriter, r *http.Request) error {
	ls, err := listing.ListSellerListings(r.Context(), s.DB, httpx.MustUser(r).UserID)
	if err != nil {
		return err
	}
	return ok(w, ls)
}

func (s *Server) sellerListing(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	l, err := listing.GetListing(r.Context(), s.DB, id)
	if err != nil {
		return err
	}
	if l.SellerID != httpx.MustUser(r).UserID {
		return listing.ErrListingNotFound
	}
	hist, err := listing.PriceHistory(r.Context(), s.DB, id)
	if err != nil {
		return err
	}
	return ok(w, map[string]any{"listing": l, "price_history": hist})
}

func (s *Server) listingAction(fn func(r *http.Request, sellerID, id int64) (any, error)) httpx.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		id, err := httpx.PathID(r, "id")
		if err != nil {
			return err
		}
		v, err := fn(r, httpx.MustUser(r).UserID, id)
		if err != nil {
			return err
		}
		return ok(w, v)
	}
}

func (s *Server) acceptQuote(w http.ResponseWriter, r *http.Request) error {
	return s.listingAction(func(r *http.Request, seller, id int64) (any, error) {
		return s.Listings.AcceptQuote(r.Context(), seller, id)
	})(w, r)
}

func (s *Server) rejectQuote(w http.ResponseWriter, r *http.Request) error {
	return s.listingAction(func(r *http.Request, seller, id int64) (any, error) {
		return map[string]string{"status": "closed"}, s.Listings.RejectQuote(r.Context(), seller, id)
	})(w, r)
}

func (s *Server) changePrice(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Multiplier float64 `json:"multiplier"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	return s.listingAction(func(r *http.Request, seller, id int64) (any, error) {
		return s.Listings.ChangePrice(r.Context(), seller, id, in.Multiplier)
	})(w, r)
}

func (s *Server) updateListingMeta(w http.ResponseWriter, r *http.Request) error {
	var in listing.UpdateListingMetaInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	return s.listingAction(func(r *http.Request, seller, id int64) (any, error) {
		return s.Listings.UpdateListingMeta(r.Context(), seller, id, in)
	})(w, r)
}

func (s *Server) setDefaultPool(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Join bool `json:"join"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	return s.listingAction(func(r *http.Request, seller, id int64) (any, error) {
		return s.Listings.SetJoinDefaultPool(r.Context(), seller, id, in.Join)
	})(w, r)
}

func (s *Server) pauseListing(w http.ResponseWriter, r *http.Request) error {
	return s.listingAction(func(r *http.Request, seller, id int64) (any, error) {
		return s.Listings.PauseListing(r.Context(), seller, id)
	})(w, r)
}

func (s *Server) resumeListing(w http.ResponseWriter, r *http.Request) error {
	return s.listingAction(func(r *http.Request, seller, id int64) (any, error) {
		return s.Listings.ResumeListing(r.Context(), seller, id)
	})(w, r)
}

func (s *Server) closeListing(w http.ResponseWriter, r *http.Request) error {
	return s.listingAction(func(r *http.Request, seller, id int64) (any, error) {
		return map[string]string{"status": "closed"}, s.Listings.CloseListing(r.Context(), seller, id)
	})(w, r)
}

// ---- 卖家：测试报告 ----

func (s *Server) sellerReports(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	limit, offset := page(r)
	rs, err := quality.SellerReports(r.Context(), s.DB, httpx.MustUser(r).UserID, id, limit, offset)
	if err != nil {
		return err
	}
	return ok(w, rs)
}

func (s *Server) sellerProbeRuns(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	limit, offset := page(r)
	runs, err := quality.SellerProbeRuns(r.Context(), s.DB, httpx.MustUser(r).UserID, id, limit, offset)
	if err != nil {
		return err
	}
	return ok(w, runs)
}

func (s *Server) sellerReport(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	rep, err := quality.SellerReport(r.Context(), s.DB, httpx.MustUser(r).UserID, id)
	if err != nil {
		return err
	}
	return ok(w, rep)
}

func (s *Server) manualRetest(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	runID, err := s.Quality.ManualRetest(r.Context(), httpx.MustUser(r).UserID, id)
	if err != nil {
		return err
	}
	return created(w, map[string]any{"run_id": runID, "note": "the check consumes a small amount of your window quota"})
}

func (s *Server) appeal(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var in struct {
		Reason string `json:"reason"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	a, err := quality.CreateAppeal(r.Context(), s.DB, httpx.MustUser(r).UserID, id, in.Reason)
	if err != nil {
		return err
	}
	return created(w, a)
}

// ---- 卖家：收益与提现 ----

func (s *Server) earnings(w http.ResponseWriter, r *http.Request) error {
	e, err := billing.SellerEarnings(r.Context(), s.DB, httpx.MustUser(r).UserID)
	if err != nil {
		return err
	}
	return ok(w, e)
}

func (s *Server) earningLines(w http.ResponseWriter, r *http.Request) error {
	limit, offset := page(r)
	lines, err := billing.SellerLines(r.Context(), s.DB, httpx.MustUser(r).UserID, queryInt64(r, "upstream_id"), limit, offset)
	if err != nil {
		return err
	}
	return ok(w, lines)
}

func (s *Server) requestWithdrawal(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Amount      float64 `json:"amount"`
		Channel     string  `json:"channel"`
		AccountInfo string  `json:"account_info"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	wd, err := s.Funds.RequestWithdrawal(r.Context(), httpx.MustUser(r).UserID, in.Amount, in.Channel, in.AccountInfo)
	if err != nil {
		return err
	}
	return created(w, wd)
}

func (s *Server) listWithdrawals(w http.ResponseWriter, r *http.Request) error {
	limit, offset := page(r)
	ws, err := s.Funds.ListWithdrawals(r.Context(), httpx.MustUser(r).UserID, "", false, limit, offset)
	if err != nil {
		return err
	}
	return ok(w, ws)
}

func (s *Server) upstreamQuota(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	q, err := s.Listings.AccountQuota(r.Context(), httpx.MustUser(r).UserID, id, r.URL.Query().Get("refresh") == "1")
	if err != nil {
		return err
	}
	return ok(w, q)
}

func (s *Server) resetCredits(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	rc, err := s.Listings.ResetCredits(r.Context(), httpx.MustUser(r).UserID, id, r.URL.Query().Get("refresh") == "1")
	if err != nil {
		return err
	}
	return ok(w, rc)
}

func (s *Server) redeemResetCredit(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var in listing.RedeemInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	// 用卡包含一次官方请求与 sub2api 的后续处理，可能超过服务默认的写超时
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(2 * time.Minute))
	e, err := s.Listings.RedeemResetCredit(r.Context(), httpx.MustUser(r).UserID, id, in)
	if err != nil {
		return err
	}
	return ok(w, e)
}

func (s *Server) setAutoReset(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var in listing.AutoResetInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	u, err := s.Listings.SetAutoReset(r.Context(), httpx.MustUser(r).UserID, id, in)
	if err != nil {
		return err
	}
	return ok(w, u)
}

func (s *Server) sellerUsage(w http.ResponseWriter, r *http.Request) error {
	upstreamID := queryInt64(r, "upstream_id")
	uid := httpx.MustUser(r).UserID
	if upstreamID > 0 {
		if _, err := listing.GetSellerUpstream(r.Context(), s.DB, uid, upstreamID); err != nil {
			return err
		}
	}
	u, err := billing.SellerUsageSummary(r.Context(), s.DB, uid, upstreamID, httpx.QueryInt(r, "days", 30, 1, 90))
	if err != nil {
		return err
	}
	return ok(w, u)
}

func (s *Server) sellerUsageRecords(w http.ResponseWriter, r *http.Request) error {
	limit, offset := page(r)
	q := r.URL.Query()
	f := billing.UsageRecordFilter{UpstreamID: queryInt64(r, "upstream_id"), Channel: q.Get("channel"), Model: q.Get("model"),
		From: queryTime(r, "from"), To: queryTime(r, "to")}
	switch f.Channel {
	case "", "buyer", "probe", "pool", "direct":
	default:
		return httpx.BadRequest("INVALID_CHANNEL", "channel must be buyer, probe, pool or direct")
	}
	rs, err := billing.SellerUsageRecords(r.Context(), s.DB, httpx.MustUser(r).UserID, f, limit, offset)
	if err != nil {
		return err
	}
	return ok(w, rs)
}
