package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/DueGin/token-market/internal/account"
	"github.com/DueGin/token-market/internal/billing"
	"github.com/DueGin/token-market/internal/buyer"
	"github.com/DueGin/token-market/internal/listing"
	"github.com/DueGin/token-market/internal/market"
	"github.com/DueGin/token-market/internal/platform/httpx"
)

// ---- 账号 ----

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

var errTooManyAttempts = httpx.TooMany("TOO_MANY_ATTEMPTS", "too many attempts, please retry later")

func (s *Server) register(w http.ResponseWriter, r *http.Request) error {
	var in credentials
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	ip := httpx.ClientIP(r)
	if s.registersByIP.Blocked(ip) {
		return errTooManyAttempts
	}
	s.registersByIP.Fail(ip)
	u, err := s.Accounts.Register(r.Context(), in.Email, in.Password, ip)
	if err != nil {
		return err
	}
	return created(w, u)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) error {
	var in credentials
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	ip, email := httpx.ClientIP(r), "email:"+strings.ToLower(strings.TrimSpace(in.Email))
	if s.loginFailsByIP.Blocked(ip) || s.loginFailsByEmail.Blocked(email) {
		return errTooManyAttempts
	}
	tok, exp, u, err := s.Accounts.Login(r.Context(), in.Email, in.Password, ip)
	if errors.Is(err, account.ErrInvalidCredentials) {
		s.loginFailsByIP.Fail(ip)
		s.loginFailsByEmail.Fail(email)
	}
	if err != nil {
		return err
	}
	s.loginFailsByEmail.Reset(email)
	return ok(w, map[string]any{"token": tok, "expires_at": exp, "user": u})
}

func (s *Server) refreshToken(w http.ResponseWriter, r *http.Request) error {
	tok, exp, u, err := s.Accounts.Refresh(r.Context(), httpx.MustUser(r).UserID)
	if err != nil {
		return err
	}
	return ok(w, map[string]any{"token": tok, "expires_at": exp, "user": u})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) error {
	u, err := s.Accounts.Get(r.Context(), httpx.MustUser(r).UserID)
	if err != nil {
		return err
	}
	return ok(w, u)
}

func (s *Server) submitKYC(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		RealName string `json:"real_name"`
		IDNumber string `json:"id_number"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if err := s.Accounts.SubmitKYC(r.Context(), httpx.MustUser(r).UserID, in.RealName, in.IDNumber); err != nil {
		return err
	}
	return ok(w, map[string]string{"status": "pending"})
}

// ---- 市场 ----

func (s *Server) marketListings(w http.ResponseWriter, r *http.Request) error {
	limit, offset := page(r)
	maxPrice, _ := strconv.ParseFloat(r.URL.Query().Get("max_price"), 64)
	items, err := s.Market.List(r.Context(), market.Filter{
		Platform: r.URL.Query().Get("platform"), Grade: r.URL.Query().Get("grade"), MaxPrice: maxPrice,
		Kind: r.URL.Query().Get("kind"), Category: r.URL.Query().Get("category"), Sort: r.URL.Query().Get("sort"),
	}, limit, offset)
	if err != nil {
		return err
	}
	return ok(w, items)
}

func (s *Server) marketListing(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	it, err := s.Market.Get(r.Context(), id)
	if err != nil {
		return err
	}
	return ok(w, it)
}

func (s *Server) marketPools(w http.ResponseWriter, r *http.Request) error {
	pools, err := s.Market.Pools(r.Context())
	if err != nil {
		return err
	}
	return ok(w, pools)
}

// marketConfig 返回前端需要的公开参数。
func (s *Server) marketConfig(w http.ResponseWriter, r *http.Request) error {
	st, err := s.settings(r.Context())
	if err != nil {
		return err
	}
	channels, err := s.Funds.RechargeChannels(r.Context())
	if err != nil {
		return err
	}
	return ok(w, map[string]any{
		"edge_url": s.EdgeURL, "commission_rate": st.CommissionRate, "freeze_days": st.FreezeDays,
		"withdraw_min": st.WithdrawMin, "max_hops": 5,
		"require_seller_proxy": st.RequireSellerProxy,
		"payment_channels":     channels, "recharge_min": st.RechargeMin, "recharge_max": st.RechargeMax,
	})
}

func (s *Server) marketPrices(w http.ResponseWriter, r *http.Request) error {
	if s.Market.Prices == nil {
		return ok(w, []any{})
	}
	prices, err := s.Market.Prices.List(r.Context())
	if err != nil {
		return httpx.Unavailable("PRICES_UNAVAILABLE", "official price list is temporarily unavailable")
	}
	return ok(w, prices)
}

type vendorView struct {
	Platform       string   `json:"platform"`
	AuthTypes      []string `json:"auth_types"`
	ConsignmentMin float64  `json:"consignment_min"`
	ConsignmentMax float64  `json:"consignment_max"`
	SelfReserve    bool     `json:"self_reserve_supported"`
}

func (s *Server) marketVendors(w http.ResponseWriter, r *http.Request) error {
	vs, err := listing.ListVendors(r.Context(), s.DB)
	if err != nil {
		return err
	}
	var out []vendorView
	for _, v := range vs {
		if v.Enabled {
			out = append(out, vendorView{v.Platform, v.AuthTypes, v.ConsignmentMin, v.ConsignmentMax, listing.SupportsSelfReserve(v.Platform)})
		}
	}
	return ok(w, out)
}

// ---- 买家：资金 ----

func (s *Server) wallet(w http.ResponseWriter, r *http.Request) error {
	wl, err := s.Funds.Wallet(r.Context(), httpx.MustUser(r).UserID)
	if err != nil {
		return err
	}
	return ok(w, wl)
}

func (s *Server) createRecharge(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Amount  float64 `json:"amount"`
		Channel string  `json:"channel"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	uid := httpx.MustUser(r).UserID
	o, err := s.Funds.CreateRecharge(r.Context(), uid, in.Amount, in.Channel, httpx.ClientIP(r))
	if err != nil {
		return err
	}
	s.Accounts.RecordIP(r.Context(), uid, httpx.ClientIP(r), "recharge")
	return created(w, map[string]any{"order": o, "payment": o.Payment})
}

func (s *Server) getRecharge(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	o, err := s.Funds.RefreshRecharge(r.Context(), httpx.MustUser(r).UserID, id)
	if err != nil {
		return err
	}
	return ok(w, o)
}

func (s *Server) cancelRecharge(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	if err := s.Funds.CancelRecharge(r.Context(), httpx.MustUser(r).UserID, id); err != nil {
		return err
	}
	return ok(w, map[string]string{"status": "cancelled"})
}

func (s *Server) requestRefund(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Note string `json:"note"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	ref, err := s.Funds.RequestRefund(r.Context(), httpx.MustUser(r).UserID, in.Note)
	if err != nil {
		return err
	}
	return created(w, ref)
}

func (s *Server) listRefunds(w http.ResponseWriter, r *http.Request) error {
	limit, offset := page(r)
	rs, err := s.Funds.ListRefunds(r.Context(), httpx.MustUser(r).UserID, "", limit, offset)
	if err != nil {
		return err
	}
	return ok(w, rs)
}

func (s *Server) purchasePack(w http.ResponseWriter, r *http.Request) error {
	var in buyer.PurchasePackInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	out, err := s.Keys.PurchasePack(r.Context(), httpx.MustUser(r).UserID, in, httpx.ClientIP(r))
	if err != nil {
		return err
	}
	return created(w, out)
}

func (s *Server) marketDiscovery(w http.ResponseWriter, r *http.Request) error {
	cfg, err := market.LoadDiscovery(r.Context(), s.DB)
	if err != nil {
		return err
	}
	return ok(w, cfg)
}

func (s *Server) marketRecommend(w http.ResponseWriter, r *http.Request) error {
	pools, err := s.Market.Pools(r.Context())
	if err != nil {
		return err
	}
	out, err := s.Market.BuildRecommendations(r.Context(), pools)
	if err != nil {
		return err
	}
	return ok(w, out)
}

func (s *Server) listingCategories(w http.ResponseWriter, r *http.Request) error {
	opts, err := listing.SellerCategoryOptions(r.Context(), s.DB)
	if err != nil {
		return err
	}
	return ok(w, opts)
}

func (s *Server) marketCategories(w http.ResponseWriter, r *http.Request) error {
	tree, err := listing.CategoryTree(r.Context(), s.DB, false)
	if err != nil {
		return err
	}
	return ok(w, tree)
}

func (s *Server) listPacks(w http.ResponseWriter, r *http.Request) error {
	ps, err := s.Keys.ListPacks(r.Context(), httpx.MustUser(r).UserID)
	if err != nil {
		return err
	}
	return ok(w, ps)
}

func (s *Server) refundPack(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var in struct {
		Dest string `json:"dest"`
	}
	_ = httpx.Decode(r, &in)
	p, err := s.Keys.RefundPack(r.Context(), httpx.MustUser(r).UserID, id, in.Dest)
	if err != nil {
		return err
	}
	return ok(w, p)
}

// ---- 买家：Key ----

func (s *Server) createKey(w http.ResponseWriter, r *http.Request) error {
	var in buyer.KeyInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	plain, k, err := s.Keys.Create(r.Context(), httpx.MustUser(r).UserID, in)
	if err != nil {
		return err
	}
	return created(w, map[string]any{"key": plain, "note": "store this key now; it will not be shown again", "detail": k})
}

func (s *Server) listKeys(w http.ResponseWriter, r *http.Request) error {
	ks, err := s.Keys.List(r.Context(), httpx.MustUser(r).UserID)
	if err != nil {
		return err
	}
	return ok(w, ks)
}

func (s *Server) getKey(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	k, err := s.Keys.Get(r.Context(), httpx.MustUser(r).UserID, id)
	if err != nil {
		return err
	}
	return ok(w, k)
}

func (s *Server) updateKey(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var in buyer.UpdateKeyInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	k, err := s.Keys.Update(r.Context(), httpx.MustUser(r).UserID, id, in)
	if err != nil {
		return err
	}
	return ok(w, k)
}

func (s *Server) setKeyEnabled(enabled bool) httpx.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		id, err := httpx.PathID(r, "id")
		if err != nil {
			return err
		}
		k, err := s.Keys.SetEnabled(r.Context(), httpx.MustUser(r).UserID, id, enabled)
		if err != nil {
			return err
		}
		return ok(w, k)
	}
}

func (s *Server) deleteKey(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	if err := s.Keys.Delete(r.Context(), httpx.MustUser(r).UserID, id); err != nil {
		return err
	}
	return ok(w, map[string]string{"status": "deleted"})
}

// ---- 买家：用量与账单 ----

func (s *Server) buyerUsage(w http.ResponseWriter, r *http.Request) error {
	limit, offset := page(r)
	from := queryTime(r, "from")
	if from.IsZero() {
		from = time.Now().Add(-7 * 24 * time.Hour)
	}
	items, err := billing.BuyerUsage(r.Context(), s.DB, httpx.MustUser(r).UserID, billing.UsageFilter{
		KeyID: queryInt64(r, "key_id"), ListingID: queryInt64(r, "listing_id"), Model: r.URL.Query().Get("model"),
		From: from, To: queryTime(r, "to"),
	}, limit, offset)
	if err != nil {
		return err
	}
	return ok(w, items)
}

func (s *Server) buyerRequestDetail(w http.ResponseWriter, r *http.Request) error {
	items, err := billing.RequestDetail(r.Context(), s.DB, httpx.MustUser(r).UserID, r.PathValue("rid"))
	if err != nil {
		return err
	}
	return ok(w, items)
}

func (s *Server) buyerBills(w http.ResponseWriter, r *http.Request) error {
	days := httpx.QueryInt(r, "days", 30, 1, 366)
	bills, err := billing.BuyerBills(r.Context(), s.DB, httpx.MustUser(r).UserID, days)
	if err != nil {
		return err
	}
	return ok(w, bills)
}
