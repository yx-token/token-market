//go:build integration

package promo

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yx-token/token-market/market/internal/platform/db"
	"github.com/yx-token/token-market/market/internal/testutil"
	"github.com/yx-token/token-market/market/migrations"
)

func newPromoService(t *testing.T) *Service {
	t.Helper()
	_, pool := testutil.NewMarketDatabase(t)
	return &Service{DB: pool}
}

func promoExec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("fixture SQL: %v", err)
	}
}

func promoUser(t *testing.T, s *Service, email string, createdAt time.Time) int64 {
	t.Helper()
	var id int64
	if err := s.DB.QueryRow(context.Background(), `INSERT INTO users(email,password_hash,created_at) VALUES($1,'fixture',$2) RETURNING id`, email, createdAt).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func promoChannel(t *testing.T, s *Service, code string) *Channel {
	t.Helper()
	c, err := s.Create(context.Background(), Input{Name: code, Code: code}, 1)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestChannelLifecycleIntegration(t *testing.T) {
	s := newPromoService(t)
	ctx := context.Background()
	c, err := s.Create(ctx, Input{Name: " Campaign ", Code: " V2EX-POST ", Note: "first"}, 42)
	if err != nil {
		t.Fatal(err)
	}
	if c.Code != "v2ex-post" || c.Name != "Campaign" || c.LandingPath != "/" || c.Status != "active" || c.Stats != zeroStats() {
		t.Fatalf("created = %+v", c)
	}
	var actor int64
	if err := s.DB.QueryRow(ctx, `SELECT created_by FROM promo_channels WHERE id=$1`, c.ID).Scan(&actor); err != nil || actor != 42 {
		t.Fatalf("actor = %d: %v", actor, err)
	}
	_, err = s.Create(ctx, Input{Name: "duplicate", Code: "V2EX-POST"}, 42)
	assertHTTPError(t, err, http.StatusConflict, "PROMO_CODE_TAKEN")
	c, err = s.Update(ctx, c.ID, Input{Name: "updated", Code: "ignored-new-code", LandingPath: "/market?sort=price", Note: "new note"})
	if err != nil || c.Code != "v2ex-post" || c.Name != "updated" || c.Note != "new note" || c.LandingPath != "/market?sort=price" {
		t.Fatalf("updated = %+v: %v", c, err)
	}
	c, err = s.SetStatus(ctx, c.ID, "disabled")
	if err != nil || c.Status != "disabled" {
		t.Fatalf("disable = %+v: %v", c, err)
	}
	_, err = s.SetStatus(ctx, c.ID, "unknown")
	assertHTTPError(t, err, http.StatusBadRequest, "INVALID_PROMO_STATUS")
	auto, err := s.Create(ctx, Input{Name: "automatic"}, 42)
	if err != nil || len(auto.Code) != 6 || !ValidCode(auto.Code) || strings.ContainsAny(auto.Code, "_-") {
		t.Fatalf("generated code = %+v: %v", auto, err)
	}
	if err := s.Delete(ctx, auto.ID); err != nil {
		t.Fatal(err)
	}
	assertHTTPError(t, s.Delete(ctx, auto.ID), http.StatusNotFound, "PROMO_CHANNEL_NOT_FOUND")
	_, err = s.Update(ctx, auto.ID, Input{Name: "missing"})
	assertHTTPError(t, err, http.StatusNotFound, "PROMO_CHANNEL_NOT_FOUND")
	_, err = s.SetStatus(ctx, auto.ID, "active")
	assertHTTPError(t, err, http.StatusNotFound, "PROMO_CHANNEL_NOT_FOUND")
	_, err = s.Daily(ctx, auto.ID, Range{})
	assertHTTPError(t, err, http.StatusNotFound, "PROMO_CHANNEL_NOT_FOUND")
	_, err = s.Users(ctx, auto.ID, 20, 0)
	assertHTTPError(t, err, http.StatusNotFound, "PROMO_CHANNEL_NOT_FOUND")
}

func TestVisitDeduplicationAndTruncationIntegration(t *testing.T) {
	s := newPromoService(t)
	ctx := context.Background()
	c := promoChannel(t, s, "visit-test")
	other := promoChannel(t, s, "other-visit")
	long := strings.Repeat("\u754c", 513)
	visit := Visit{Code: c.Code, VisitorID: "Visitor_123", LandingPath: "/", Referrer: long}
	const concurrent = 24
	type result struct {
		accepted bool
		err      error
	}
	results := make(chan result, concurrent)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for range concurrent {
		workers.Go(func() {
			<-start
			accepted, err := s.RecordVisit(ctx, visit, "192.0.2.1", long)
			results <- result{accepted, err}
		})
	}
	close(start)
	workers.Wait()
	close(results)
	acceptedCount := 0
	for got := range results {
		if got.err != nil {
			t.Fatal(got.err)
		}
		if got.accepted {
			acceptedCount++
		}
	}
	if acceptedCount != 1 {
		t.Fatalf("concurrent accepted = %d, want 1", acceptedCount)
	}
	var rows, uaLength, refLength int
	if err := s.DB.QueryRow(ctx, `SELECT count(*),max(char_length(user_agent)),max(char_length(referrer)) FROM promo_channel_visits WHERE channel_id=$1`, c.ID).Scan(&rows, &uaLength, &refLength); err != nil || rows != 1 || uaLength != 512 || refLength != 512 {
		t.Fatalf("saved visits = %d ua=%d ref=%d: %v", rows, uaLength, refLength, err)
	}
	promoExec(t, s.DB, `UPDATE promo_channel_visits SET created_at=now()-interval '9 minutes' WHERE channel_id=$1`, c.ID)
	if accepted, err := s.RecordVisit(ctx, visit, "192.0.2.2", "ua"); err != nil || accepted {
		t.Fatalf("within deduplication window = %v: %v", accepted, err)
	}
	promoExec(t, s.DB, `UPDATE promo_channel_visits SET created_at=now()-interval '11 minutes' WHERE channel_id=$1`, c.ID)
	if accepted, err := s.RecordVisit(ctx, visit, "192.0.2.2", "ua"); err != nil || !accepted {
		t.Fatalf("after deduplication window = %v: %v", accepted, err)
	}
	visit.Code = other.Code
	if accepted, err := s.RecordVisit(ctx, visit, "192.0.2.1", "ua"); err != nil || !accepted {
		t.Fatalf("same visitor in another channel = %v: %v", accepted, err)
	}
	visit.Code, visit.VisitorID = c.Code, "Visitor_456"
	if accepted, err := s.RecordVisit(ctx, visit, "192.0.2.1", "ua"); err != nil || !accepted {
		t.Fatalf("different visitor = %v: %v", accepted, err)
	}
	if _, err := s.SetStatus(ctx, other.ID, "disabled"); err != nil {
		t.Fatal(err)
	}
	for _, input := range []Visit{{Code: "missing-code", VisitorID: "Visitor_123"}, {Code: other.Code, VisitorID: "Visitor_456"}, {Code: c.Code, VisitorID: "short"}, {Code: "BAD.CODE", VisitorID: "Visitor_123"}} {
		if accepted, err := s.RecordVisit(ctx, input, "192.0.2.1", "ua"); err != nil || accepted {
			t.Fatalf("invalid or inactive visit %+v accepted=%v: %v", input, accepted, err)
		}
	}
	assertHTTPError(t, s.Delete(ctx, c.ID), http.StatusConflict, "PROMO_CHANNEL_HAS_DATA")
}

func TestAttributionIntegration(t *testing.T) {
	s := newPromoService(t)
	ctx := context.Background()
	active := promoChannel(t, s, "attribution")
	other := promoChannel(t, s, "other-attribution")
	disabled := promoChannel(t, s, "disabled-attribution")
	if _, err := s.SetStatus(ctx, disabled.ID, "disabled"); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	for i, tc := range []struct {
		name, code, visitor, touched string
		attributed, badRequest       bool
		clampedTouch                 bool
	}{
		{name: "valid", code: active.Code, visitor: "Visitor_123", touched: now.Add(-29 * 24 * time.Hour).Format(time.RFC3339Nano), attributed: true},
		{name: "no touch", code: active.Code, attributed: true},
		{name: "expired", code: active.Code, touched: now.Add(-31 * 24 * time.Hour).Format(time.RFC3339Nano), badRequest: true},
		{name: "future clock skew", code: active.Code, touched: now.Add(time.Hour).Format(time.RFC3339Nano), attributed: true, clampedTouch: true},
		{name: "invalid time", code: active.Code, touched: "not-a-date", badRequest: true},
		{name: "disabled", code: disabled.Code},
		{name: "missing", code: "missing-code"},
		{name: "invalid code", code: "BAD.CODE", badRequest: true},
		{name: "invalid visitor", code: active.Code, visitor: "short", badRequest: true},
		{name: "organic"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registeredAt := now.Add(-time.Hour)
			userID := promoUser(t, s, fmt.Sprintf("attribution-%d@example.test", i), registeredAt)
			err := s.Attribute(ctx, userID, tc.code, tc.visitor, tc.touched)
			if tc.badRequest {
				assertHTTPError(t, err, http.StatusBadRequest, "INVALID_PROMO_ATTRIBUTION")
			} else if err != nil {
				t.Fatal(err)
			}
			var count int
			if err := s.DB.QueryRow(ctx, `SELECT count(*) FROM promo_channel_users WHERE user_id=$1`, userID).Scan(&count); err != nil || (count == 1) != tc.attributed {
				t.Fatalf("attribution rows = %d, want attributed=%v: %v", count, tc.attributed, err)
			}
			if !tc.attributed {
				return
			}
			if err := s.Attribute(ctx, userID, other.Code, "Another_123", now.Add(-time.Minute).Format(time.RFC3339Nano)); err != nil {
				t.Fatal(err)
			}
			var channelID int64
			var createdAt time.Time
			var visitor *string
			var touched *time.Time
			if err := s.DB.QueryRow(ctx, `SELECT channel_id,created_at,visitor_id,touched_at FROM promo_channel_users WHERE user_id=$1`, userID).Scan(&channelID, &createdAt, &visitor, &touched); err != nil {
				t.Fatal(err)
			}
			if channelID != active.ID || !createdAt.Equal(registeredAt) || (visitor == nil) != (tc.visitor == "") || (touched == nil) != (tc.touched == "") {
				t.Fatalf("immutable attribution = channel %d, created %v, visitor %v, touch %v", channelID, createdAt, visitor, touched)
			}
			if visitor != nil && *visitor != tc.visitor {
				t.Fatalf("visitor changed: %q", *visitor)
			}
			if tc.clampedTouch {
				if touched == nil || touched.After(time.Now()) || touched.Before(now) {
					t.Fatalf("future touch should be clamped to server time, got %v", touched)
				}
			} else if touched != nil && !touched.Equal(mustPromoTime(t, tc.touched)) {
				t.Fatalf("touch changed: %v, want %s", touched, tc.touched)
			}
		})
	}
	assertHTTPError(t, s.Delete(ctx, active.ID), http.StatusConflict, "PROMO_CHANNEL_HAS_DATA")
	concurrentUser := promoUser(t, s, "concurrent-attribution@example.test", now)
	const concurrent = 20
	errors := make(chan error, concurrent)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for i := range concurrent {
		code := active.Code
		if i%2 == 1 {
			code = other.Code
		}
		workers.Go(func() {
			<-start
			errors <- s.Attribute(ctx, concurrentUser, code, "Visitor_123", "")
		})
	}
	close(start)
	workers.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := s.DB.QueryRow(ctx, `SELECT count(*) FROM promo_channel_users WHERE user_id=$1 AND channel_id IN ($2,$3)`, concurrentUser, active.ID, other.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("concurrent attribution rows = %d, want 1: %v", count, err)
	}
}

func TestPromoMigrationPreservesExistingUsersAndMenuIntegration(t *testing.T) {
	_, pool := testutil.NewDatabase(t)
	ctx := context.Background()
	legacy := fstest.MapFS{}
	names, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if name >= "0050" {
			continue
		}
		content, err := migrations.FS.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		legacy[name] = &fstest.MapFile{Data: content}
	}
	if _, err := db.Migrate(ctx, pool, legacy); err != nil {
		t.Fatal(err)
	}
	promoExec(t, pool, `INSERT INTO users(email,password_hash) VALUES('legacy@example.test','unchanged')`)
	// An existing resource must not be overwritten when this migration adds its grants.
	promoExec(t, pool, `INSERT INTO iam_resources(id,parent_id,name,kind,path) VALUES('console.ops.promo','console.ops','custom title','menu','/custom')`)
	applied, err := db.Migrate(ctx, pool, migrations.FS)
	if err != nil || len(applied) != 1 || applied[0] != "0050_promo_channels.sql" {
		t.Fatalf("new migration = %v: %v", applied, err)
	}
	applied, err = db.Migrate(ctx, pool, migrations.FS)
	if err != nil || len(applied) != 0 {
		t.Fatalf("migration rerun = %v: %v", applied, err)
	}
	var name, path, password string
	if err := pool.QueryRow(ctx, `SELECT name,path FROM iam_resources WHERE id='console.ops.promo'`).Scan(&name, &path); err != nil || name != "custom title" || path != "/custom" {
		t.Fatalf("existing menu overwritten: %s %s %v", name, path, err)
	}
	if err := pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE email='legacy@example.test'`).Scan(&password); err != nil || password != "unchanged" {
		t.Fatalf("legacy user changed: %s %v", password, err)
	}
	var grants int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM iam_role_resources rr JOIN iam_roles r ON r.id=rr.role_id WHERE rr.resource_id='console.ops.promo' AND r.code IN ('platform_admin','operator')`).Scan(&grants); err != nil || grants != 2 {
		t.Fatalf("menu role grants = %d: %v", grants, err)
	}
	fresh := newPromoService(t)
	var parent, kind string
	var enabled bool
	if err := fresh.DB.QueryRow(ctx, `SELECT parent_id,kind,path,enabled FROM iam_resources WHERE id='console.ops.promo'`).Scan(&parent, &kind, &path, &enabled); err != nil || parent != "console.ops" || kind != "menu" || path != "/admin/market/promo-channels" || !enabled {
		t.Fatalf("fresh menu: parent=%s kind=%s path=%s enabled=%v: %v", parent, kind, path, enabled, err)
	}
	for _, index := range []string{"promo_visits_channel_time_idx", "promo_visits_channel_visitor_idx", "promo_users_channel_time_idx"} {
		var exists bool
		if err := fresh.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_indexes WHERE schemaname='public' AND indexname=$1)`, index).Scan(&exists); err != nil || !exists {
			t.Fatalf("missing index %s: %v", index, err)
		}
	}
}
