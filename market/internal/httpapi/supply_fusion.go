package httpapi

import (
	"net/http"

	"github.com/yx-token/token-market/market/internal/listing"
	"github.com/yx-token/token-market/market/internal/platform/usermail"
	"github.com/yx-token/token-market/market/internal/scheduling"
	"github.com/yx-token/token-market/market/internal/sub2api/replica"
)

// link_state 取值：linked / unbound / orphan / engine_only
const (
	linkLinked     = "linked"
	linkUnbound    = "unbound"
	linkOrphan     = "orphan"
	linkEngineOnly = "engine_only"
)

// fusedAccount 是一条供给：闲猫上游与 sub2api 账号是同一资源。
type fusedAccount struct {
	UpstreamID      int64    `json:"upstream_id,omitempty"`
	UpstreamCode    string   `json:"upstream_code,omitempty"`
	UpstreamName    string   `json:"upstream_name,omitempty"`
	AccountEmail    string   `json:"account_email,omitempty"`
	SellerID        int64    `json:"seller_id,omitempty"`
	SellerEmail     string   `json:"seller_email,omitempty"`
	Platform        string   `json:"platform"`
	MarketStatus    string   `json:"market_status"`
	Health          string   `json:"health,omitempty"`
	Grade           *string  `json:"grade,omitempty"`
	EngineAccountID *int64   `json:"engine_account_id,omitempty"`
	EngineName      string   `json:"engine_name,omitempty"`
	EngineStatus    string   `json:"engine_status,omitempty"`
	Schedulable     *bool    `json:"schedulable,omitempty"`
	RateMultiplier  *float64 `json:"rate_multiplier,omitempty"`
	Concurrency     *int     `json:"concurrency,omitempty"`
	Linked          bool     `json:"linked"`
	LinkState       string   `json:"link_state"`
}

// fusedGroup 是一条供给分组：闲猫默认分组与 sub2api 分组是同一资源。
type fusedGroup struct {
	PoolID         int64    `json:"pool_id,omitempty"`
	Name           string   `json:"name"`
	Platform       string   `json:"platform"`
	Enabled        *bool    `json:"enabled,omitempty"`
	SellMultiplier *float64 `json:"sell_multiplier,omitempty"`
	EngineGroupID  *int64   `json:"engine_group_id,omitempty"`
	EngineName     string   `json:"engine_name,omitempty"`
	EngineRate     *float64 `json:"engine_rate,omitempty"`
	MarketStatus   string   `json:"market_status"`
	Linked         bool     `json:"linked"`
	LinkState      string   `json:"link_state"`
}

func (s *Server) adminSupplyAccounts(w http.ResponseWriter, r *http.Request) error {
	ups, err := listing.ListUpstreams(r.Context(), s.DB, "", 500, 0)
	if err != nil {
		return err
	}
	var engines []replica.Account
	if s.Replica != nil {
		engines, err = s.Replica.TMAccounts(r.Context())
		if err != nil {
			return err
		}
	}
	out := fuseAccounts(ups, engines)
	ids := make([]int64, 0, len(out))
	for _, row := range out {
		if row.SellerID > 0 {
			ids = append(ids, row.SellerID)
		}
	}
	emails, err := usermail.Lookup(r.Context(), s.DB, ids)
	if err != nil {
		return err
	}
	for i := range out {
		out[i].SellerEmail = emails[out[i].SellerID]
	}
	return ok(w, out)
}

func (s *Server) adminSupplyGroups(w http.ResponseWriter, r *http.Request) error {
	pools, err := scheduling.ListPools(r.Context(), s.DB, false)
	if err != nil {
		return err
	}
	var groups []replica.Group
	if s.Replica != nil {
		groups, err = s.Replica.TMGroups(r.Context())
		if err != nil {
			return err
		}
	}
	return ok(w, fuseGroups(pools, groups))
}

func fuseAccounts(ups []listing.Upstream, engines []replica.Account) []fusedAccount {
	byID := make(map[int64]replica.Account, len(engines))
	for _, a := range engines {
		byID[a.ID] = a
	}
	used := map[int64]bool{}
	out := make([]fusedAccount, 0, len(ups)+len(engines))
	for _, u := range ups {
		row := fusedAccount{
			UpstreamID:   u.ID,
			UpstreamCode: u.Code,
			SellerID:     u.SellerID,
			Platform:     u.Platform,
			MarketStatus: u.Status,
			Health:       u.Health,
			Grade:        u.Grade,
			LinkState:    linkUnbound,
		}
		if u.Name != nil {
			row.UpstreamName = *u.Name
		}
		if u.AccountEmail != nil {
			row.AccountEmail = *u.AccountEmail
		}
		if u.Sub2APIAccountID != nil {
			id := *u.Sub2APIAccountID
			row.EngineAccountID = &id
			if a, ok := byID[id]; ok {
				fillEngineAccount(&row, a)
				used[id] = true
				row.LinkState = linkLinked
			} else {
				row.EngineStatus = "未同步"
				row.LinkState = linkOrphan
			}
		}
		out = append(out, row)
	}
	for _, a := range engines {
		if used[a.ID] {
			continue
		}
		row := fusedAccount{Platform: a.Platform, MarketStatus: "仅引擎", LinkState: linkEngineOnly}
		fillEngineAccount(&row, a)
		row.Linked = false
		row.LinkState = linkEngineOnly
		out = append(out, row)
	}
	return out
}

func fillEngineAccount(row *fusedAccount, a replica.Account) {
	id := a.ID
	sched := a.Schedulable
	rate := a.RateMultiplier
	conc := a.Concurrency
	row.EngineAccountID = &id
	row.EngineName = a.Name
	row.EngineStatus = a.Status
	row.Schedulable = &sched
	row.RateMultiplier = &rate
	row.Concurrency = &conc
	row.Linked = true
	if row.Platform == "" {
		row.Platform = a.Platform
	}
}

func fuseGroups(pools []scheduling.Pool, groups []replica.Group) []fusedGroup {
	byID := make(map[int64]replica.Group, len(groups))
	for _, g := range groups {
		byID[g.ID] = g
	}
	used := map[int64]bool{}
	out := make([]fusedGroup, 0, len(pools)+len(groups))
	for _, p := range pools {
		enabled := p.Enabled
		sell := p.SellMultiplier
		row := fusedGroup{
			PoolID:         p.ID,
			Name:           p.Name,
			Platform:       p.Platform,
			Enabled:        &enabled,
			SellMultiplier: &sell,
			MarketStatus:   "市场分组",
			LinkState:      linkUnbound,
		}
		if p.Sub2APIGroupID != nil {
			id := *p.Sub2APIGroupID
			row.EngineGroupID = &id
			if g, ok := byID[id]; ok {
				fillEngineGroup(&row, g)
				used[id] = true
				row.LinkState = linkLinked
			} else {
				row.MarketStatus = "未同步"
				row.LinkState = linkOrphan
			}
		}
		out = append(out, row)
	}
	for _, g := range groups {
		if used[g.ID] {
			continue
		}
		row := fusedGroup{Platform: g.Platform, MarketStatus: "仅引擎", LinkState: linkEngineOnly}
		fillEngineGroup(&row, g)
		row.Linked = false
		row.LinkState = linkEngineOnly
		if row.Name == "" {
			row.Name = g.Name
		}
		out = append(out, row)
	}
	return out
}

func fillEngineGroup(row *fusedGroup, g replica.Group) {
	id := g.ID
	rate := g.RateMultiplier
	row.EngineGroupID = &id
	row.EngineName = g.Name
	row.EngineRate = &rate
	row.Linked = true
	if row.Name == "" {
		row.Name = g.Name
	}
	if row.Platform == "" {
		row.Platform = g.Platform
	}
}
