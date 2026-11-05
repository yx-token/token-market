//go:build integration

package promo

import (
	"context"
	"testing"
	"time"
)

func mustPromoTime(t *testing.T, raw string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func promoRecharge(t *testing.T, s *Service, userID int64, amount, refunded, status string, paidAt, creditedAt *time.Time) {
	t.Helper()
	promoExec(t, s.DB, `INSERT INTO recharge_orders(user_id,amount,refunded_amount,provider,status,paid_at,credited_at)
		VALUES($1,$2::numeric,$3::numeric,'fixture',$4,$5,$6)`, userID, amount, refunded, status, paidAt, creditedAt)
}

func promoUsage(t *testing.T, s *Service, id, userID int64, at time.Time, actual string, cash *string, prepaid string) {
	t.Helper()
	promoExec(t, s.DB, `INSERT INTO usage_records(sub2api_usage_id,created_at,sub2api_account_id,sub2api_user_id,
		sub2api_api_key_id,model,total_cost,actual_cost,cash_cost,credit_prepaid_cny,buyer_user_id,channel)
		VALUES($1,$2,1,1,1,'fixture',100,$3::numeric,$4::numeric,$5::numeric,$6,'direct')`, id, at, actual, cash, prepaid, userID)
}

func promoListing(t *testing.T, s *Service, userID int64, at time.Time) {
	t.Helper()
	var upstreamID int64
	if err := s.DB.QueryRow(context.Background(), `INSERT INTO upstreams(seller_id,platform,auth_type)
		VALUES($1,'openai','oauth') RETURNING id`, userID).Scan(&upstreamID); err != nil {
		t.Fatal(err)
	}
	promoExec(t, s.DB, `INSERT INTO listings(upstream_id,seller_id,mode,status,created_at)
		VALUES($1,$2,'consignment','closed',$3)`, upstreamID, userID, at)
}

func assertStats(t *testing.T, got, want Stats) {
	t.Helper()
	if got != want {
		t.Fatalf("stats = %+v, want %+v", got, want)
	}
}

func TestStatsEventWindowsOrganicDailyAndUsersIntegration(t *testing.T) {
	s := newPromoService(t)
	ctx := context.Background()
	a := promoChannel(t, s, "stats-a")
	b := promoChannel(t, s, "stats-b")
	empty := promoChannel(t, s, "stats-empty")
	start := mustPromoTime(t, "2026-11-02T16:00:00Z")
	end := mustPromoTime(t, "2026-11-05T16:00:00Z")
	before := start.Add(-time.Microsecond)
	dayOne := start.Add(time.Hour)
	dayThree := end.Add(-time.Hour)
	last := end.Add(-time.Microsecond)
	old := start.AddDate(0, -1, 0)
	for i, c := range []*Channel{a, b, empty} {
		promoExec(t, s.DB, `UPDATE promo_channels SET created_at=$2 WHERE id=$1`, c.ID, old.Add(time.Duration(i)*time.Hour))
	}
	aOld := promoUser(t, s, "a-old@example.test", old)
	aNew := promoUser(t, s, "a-new@example.test", start)
	aAfter := promoUser(t, s, "a-after@example.test", end)
	bNew := promoUser(t, s, "b-new@example.test", dayThree)
	organicOld := promoUser(t, s, "organic-old@example.test", old)
	organicNew := promoUser(t, s, "organic-new@example.test", start)
	_ = promoUser(t, s, "organic-after@example.test", end)
	for _, userID := range []int64{aOld, aNew, aAfter} {
		if err := s.Attribute(ctx, userID, a.Code, "", ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Attribute(ctx, bNew, b.Code, "", ""); err != nil {
		t.Fatal(err)
	}
	// Disabling a channel must not erase its lifetime user cohort or historical events.
	if _, err := s.SetStatus(ctx, b.ID, "disabled"); err != nil {
		t.Fatal(err)
	}
	for _, visit := range []struct {
		channelID int64
		visitor   string
		at        time.Time
	}{
		{a.ID, "OutsideBefore", before}, {a.ID, "Shared_Visitor", start}, {a.ID, "Shared_Visitor", dayOne},
		{a.ID, "Second_Visitor", last}, {a.ID, "OutsideAfter", end}, {b.ID, "Shared_Visitor", dayOne},
	} {
		promoExec(t, s.DB, `INSERT INTO promo_channel_visits(channel_id,visitor_id,created_at) VALUES($1,$2,$3)`, visit.channelID, visit.visitor, visit.at)
	}
	promoRecharge(t, s, aOld, "100", "20", "credited", &old, &start)
	promoRecharge(t, s, aOld, "25", "5", "credited", &last, nil)
	promoRecharge(t, s, aNew, "5", "0", "credited", &before, &dayOne)
	promoRecharge(t, s, aOld, "1000", "0", "credited", &dayOne, &before)
	promoRecharge(t, s, aOld, "2000", "0", "credited", &dayOne, &end)
	for _, status := range []string{"pending", "crediting", "cancelled", "expired", "failed"} {
		promoRecharge(t, s, aNew, "999", "0", status, &dayOne, &dayOne)
	}
	promoRecharge(t, s, bNew, "12.50", "2.50", "credited", &dayThree, &dayThree)
	promoRecharge(t, s, organicOld, "40", "10", "credited", &dayOne, &dayOne)
	promoRecharge(t, s, organicNew, "5", "0", "credited", &dayThree, &dayThree)
	promoRecharge(t, s, organicOld, "77", "0", "credited", &before, &before)
	cashA, cashB, cashOrganic := "4.20", "0", "1"
	promoUsage(t, s, 1, aOld, start, "50", &cashA, "1.30")
	promoUsage(t, s, 2, aNew, last, "2.25", nil, "0.25")
	promoUsage(t, s, 3, aOld, before, "100", nil, "0")
	promoUsage(t, s, 4, aOld, end, "200", nil, "0")
	promoUsage(t, s, 5, bNew, dayThree, "99", &cashB, "3")
	promoUsage(t, s, 6, organicOld, dayOne, "9", &cashOrganic, "0.50")
	promoUsage(t, s, 7, organicNew, dayThree, "2.50", nil, "0.50")
	promoListing(t, s, aOld, start)
	promoListing(t, s, aOld, dayThree)
	promoListing(t, s, aNew, before)
	promoListing(t, s, aNew, end)
	promoListing(t, s, bNew, dayThree)
	promoListing(t, s, organicOld, dayOne)
	promoListing(t, s, organicNew, dayThree)
	period, err := ParseRange("2026-11-03", "2026-11-05", false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	listed, err := s.List(ctx, period)
	if err != nil {
		t.Fatal(err)
	}
	if listed.From == nil || listed.To == nil || *listed.From != "2026-11-03" || *listed.To != "2026-11-05" || len(listed.Items) != 3 {
		t.Fatalf("list range or channel count = %+v", listed)
	}
	if listed.Items[0].ID != empty.ID || listed.Items[1].ID != b.ID || listed.Items[2].ID != a.ID {
		t.Fatalf("channels not ordered newest first: %+v", listed.Items)
	}
	assertStats(t, listed.Items[0].Stats, zeroStats())
	assertStats(t, listed.Items[1].Stats, Stats{PV: 1, UV: 1, Signups: 1, PayingUsers: 1, RechargeAmount: "10.00", BuyerSpend: "3.00", SellerUsers: 1})
	assertStats(t, listed.Items[2].Stats, Stats{PV: 3, UV: 2, Signups: 1, PayingUsers: 2, RechargeAmount: "105.00", BuyerSpend: "8.00", SellerUsers: 1})
	// A visitor shared by two channels counts once in each channel, as totals sum channel stats.
	assertStats(t, listed.Totals, Stats{PV: 4, UV: 3, Signups: 2, PayingUsers: 3, RechargeAmount: "115.00", BuyerSpend: "11.00", SellerUsers: 2})
	wantOrganic := Organic{Signups: 1, PayingUsers: 2, RechargeAmount: "35.00", BuyerSpend: "4.50", SellerUsers: 2}
	if listed.Organic != wantOrganic {
		t.Fatalf("organic = %+v, want %+v", listed.Organic, wantOrganic)
	}
	all, err := s.List(ctx, Range{})
	if err != nil || all.From != nil || all.To != nil {
		t.Fatalf("all-time range = %+v: %v", all, err)
	}
	assertStats(t, all.Items[2].Stats, Stats{PV: 5, UV: 4, Signups: 3, PayingUsers: 2, RechargeAmount: "3105.00", BuyerSpend: "308.00", SellerUsers: 2})
	dailyRange, err := ParseRange("2026-11-03", "2026-11-05", true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	days, err := s.Daily(ctx, a.ID, dailyRange)
	if err != nil || len(days) != 3 {
		t.Fatalf("daily = %+v: %v", days, err)
	}
	if days[0].Date != "2026-11-03" || days[1].Date != "2026-11-04" || days[2].Date != "2026-11-05" {
		t.Fatalf("dates not ascending Beijing dates: %+v", days)
	}
	assertStats(t, days[0].Stats, Stats{PV: 2, UV: 1, Signups: 1, PayingUsers: 2, RechargeAmount: "85.00", BuyerSpend: "5.50", SellerUsers: 1})
	assertStats(t, days[1].Stats, zeroStats())
	assertStats(t, days[2].Stats, Stats{PV: 1, UV: 1, PayingUsers: 1, RechargeAmount: "20.00", BuyerSpend: "2.50", SellerUsers: 1})
	users, err := s.Users(ctx, a.ID, 20, 0)
	if err != nil || users.Total != 3 || len(users.Items) != 3 {
		t.Fatalf("users = %+v: %v", users, err)
	}
	if users.Items[0].UserID != aAfter || users.Items[1].UserID != aNew || users.Items[2].UserID != aOld {
		t.Fatalf("users not ordered by registration: %+v", users.Items)
	}
	if u := users.Items[0]; u.RechargeAmount != "0.00" || u.BuyerSpend != "0.00" || u.IsSeller {
		t.Fatalf("empty lifetime user stats = %+v", u)
	}
	if u := users.Items[1]; u.Email != "a-new@example.test" || !u.RegisteredAt.Equal(start) || u.RechargeAmount != "5.00" || u.BuyerSpend != "2.50" || !u.IsSeller {
		t.Fatalf("new lifetime user stats = %+v", u)
	}
	if u := users.Items[2]; u.RechargeAmount != "3100.00" || u.BuyerSpend != "305.50" || !u.IsSeller {
		t.Fatalf("old lifetime user stats = %+v", u)
	}
	page, err := s.Users(ctx, a.ID, 1, 1)
	if err != nil || page.Total != 3 || len(page.Items) != 1 || page.Items[0].UserID != aNew {
		t.Fatalf("user pagination = %+v: %v", page, err)
	}
	emptyUsers, err := s.Users(ctx, empty.ID, 20, 0)
	if err != nil || emptyUsers.Total != 0 || emptyUsers.Items == nil || len(emptyUsers.Items) != 0 {
		t.Fatalf("empty users must be [] with total 0: %+v: %v", emptyUsers, err)
	}
	emptyDays, err := s.Daily(ctx, empty.ID, dailyRange)
	if err != nil || len(emptyDays) != 3 {
		t.Fatalf("empty daily = %+v: %v", emptyDays, err)
	}
	for _, day := range emptyDays {
		assertStats(t, day.Stats, zeroStats())
	}
}
