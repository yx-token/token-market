package scheduling

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
	"github.com/yx-token/token-market/market/internal/sub2api/opshandlers"
	"github.com/yx-token/token-market/market/internal/sub2api/provision"
)

type poolGroupProvisionerStub struct {
	specs []provision.GroupSpec
}

func (s *poolGroupProvisionerStub) EnsureGroup(_ context.Context, spec provision.GroupSpec) (int64, error) {
	s.specs = append(s.specs, spec)
	return 200, nil
}

type poolRowStub struct {
	scan func(dest ...any) error
}

func (r poolRowStub) Scan(dest ...any) error { return r.scan(dest...) }

type poolDBStub struct {
	pool          Pool
	deleted       bool
	groupDeleting bool // 已删除的同名分组在 sub2api 中尚未删掉
	updatePayload []opshandlers.GroupUpdatePayload
	opTypes       []string
}

func (db *poolDBStub) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	switch {
	case strings.Contains(query, "UPDATE pools SET sub2api_group_id"):
		groupID := args[1].(int64)
		db.pool.Sub2APIGroupID = &groupID
	case strings.Contains(query, "UPDATE pools SET name ="):
		db.pool.Name = args[1].(string)
		db.pool.SellMultiplier = args[2].(float64)
		db.pool.MinMargin = args[3].(float64)
		db.pool.SafetyBuffer = args[4].(float64)
		db.pool.Enabled = args[5].(bool)
		db.pool.MarketPublished = args[6].(bool)
		db.pool.MarketTitle = args[7].(string)
		db.pool.MarketDescription = args[8].(string)
	case strings.Contains(query, "UPDATE pools SET deleted_at"):
		if db.deleted || db.pool.Enabled {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}
		db.deleted, db.pool.MarketPublished = true, false
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	return pgconn.CommandTag{}, nil
}

func (db *poolDBStub) Query(_ context.Context, query string, _ ...any) (pgx.Rows, error) {
	return nil, fmt.Errorf("unexpected query: %s", query)
}

func (db *poolDBStub) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	switch {
	case strings.Contains(query, "o.idempotency_key = 'pool-delete-'"):
		return poolRowStub{scan: func(dest ...any) error {
			*dest[0].(*bool) = db.groupDeleting
			return nil
		}}
	case strings.Contains(query, "INSERT INTO pools"):
		return poolRowStub{scan: func(dest ...any) error {
			*dest[0].(*int64) = db.pool.ID
			return nil
		}}
	case strings.Contains(query, "FROM pools WHERE id = $1"):
		if db.deleted && strings.Contains(query, "deleted_at IS NULL") {
			return poolRowStub{scan: func(...any) error { return pgx.ErrNoRows }}
		}
		return poolRowStub{scan: db.scanPool}
	case strings.Contains(query, "INSERT INTO sub2api_ops"):
		db.opTypes = append(db.opTypes, args[0].(string))
		payload := opshandlers.GroupUpdatePayload{}
		if err := json.Unmarshal(args[2].([]byte), &payload); err != nil {
			return poolRowStub{scan: func(...any) error { return err }}
		}
		db.updatePayload = append(db.updatePayload, payload)
		return poolRowStub{scan: func(dest ...any) error {
			*dest[0].(*int64) = 1
			return nil
		}}
	default:
		return poolRowStub{scan: func(...any) error { return fmt.Errorf("unexpected QueryRow: %s", query) }}
	}
}

func (db *poolDBStub) scanPool(dest ...any) error {
	p := db.pool
	*dest[0].(*int64) = p.ID
	*dest[1].(*string) = p.Name
	*dest[2].(*string) = p.Platform
	*dest[3].(**int64) = p.Sub2APIGroupID
	*dest[4].(*float64) = p.SellMultiplier
	*dest[5].(*float64) = p.MinMargin
	*dest[6].(*float64) = p.SafetyBuffer
	*dest[7].(*bool) = p.Enabled
	*dest[8].(*bool) = p.MarketPublished
	*dest[9].(*string) = p.MarketTitle
	*dest[10].(*string) = p.MarketDescription
	*dest[11].(*time.Time) = p.CreatedAt
	*dest[12].(*time.Time) = p.UpdatedAt
	return nil
}

func TestPoolCreateUsesEngineSupportedProfitControl(t *testing.T) {
	for platform, want := range map[string]bool{
		"anthropic": true, "openai": true, "gemini": true, "antigravity": true, "grok": true,
		"composite": false, "kimi": false, "zhipu": false, "minimax": false, "opencode_go": false,
	} {
		t.Run(platform, func(t *testing.T) {
			db := &poolDBStub{pool: Pool{ID: 1, Name: "tm-pool-test", Platform: platform, SellMultiplier: 0.6, MinMargin: 0.1, SafetyBuffer: 0.02, Enabled: true}}
			prov := &poolGroupProvisionerStub{}
			svc := &PoolService{DB: db, Prov: prov}
			_, err := svc.Create(context.Background(), PoolInput{
				Name: "tm-pool-test", Platform: platform, SellMultiplier: f64(0.6), MinMargin: f64(0.1), SafetyBuffer: f64(0.02),
			})
			if err != nil {
				t.Fatalf("Create() error = %v", err)
			}
			if len(prov.specs) != 1 {
				t.Fatalf("EnsureGroup() calls = %d, want 1", len(prov.specs))
			}
			if got := prov.specs[0].ProfitControl; got != want {
				t.Errorf("ProfitControl = %v, want %v", got, want)
			}
		})
	}
}

func TestPoolUpdateUsesEngineSupportedProfitControl(t *testing.T) {
	for platform, want := range map[string]bool{
		"anthropic": true, "composite": false, "kimi": false,
	} {
		t.Run(platform, func(t *testing.T) {
			groupID := int64(200)
			db := &poolDBStub{pool: Pool{ID: 1, Name: "tm-pool-test", Platform: platform, Sub2APIGroupID: &groupID, SellMultiplier: 0.6, MinMargin: 0.1, SafetyBuffer: 0.02, Enabled: true}}
			svc := &PoolService{DB: db, Prov: &poolGroupProvisionerStub{}}
			_, err := svc.Update(context.Background(), 1, PoolInput{SellMultiplier: f64(0.7)})
			if err != nil {
				t.Fatalf("Update() error = %v", err)
			}
			if len(db.updatePayload) != 1 {
				t.Fatalf("group update payloads = %d, want 1", len(db.updatePayload))
			}
			got := *db.updatePayload[0].Input.ProfitControlEnabled
			if got != want {
				t.Errorf("profit_control_enabled = %v, want %v", got, want)
			}
			if db.updatePayload[0].Input.Name != "" {
				t.Errorf("Name = %q, want empty when name unchanged", db.updatePayload[0].Input.Name)
			}
		})
	}
}

func TestPoolUpdateRenamesAndSyncsGroupName(t *testing.T) {
	groupID := int64(200)
	db := &poolDBStub{pool: Pool{ID: 1, Name: "tm-pool-old", Platform: "openai", Sub2APIGroupID: &groupID, SellMultiplier: 0.6, MinMargin: 0.1, SafetyBuffer: 0.02, Enabled: true}}
	svc := &PoolService{DB: db, Prov: &poolGroupProvisionerStub{}}
	_, err := svc.Update(context.Background(), 1, PoolInput{Name: "tm-pool-new"})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if db.pool.Name != "tm-pool-new" {
		t.Fatalf("pool name = %q, want tm-pool-new", db.pool.Name)
	}
	if len(db.updatePayload) != 1 {
		t.Fatalf("group update payloads = %d, want 1", len(db.updatePayload))
	}
	if got := db.updatePayload[0].Input.Name; got != "tm-pool-new" {
		t.Errorf("synced Name = %q, want tm-pool-new", got)
	}
}

func TestPoolUpdateRejectsInvalidRename(t *testing.T) {
	groupID := int64(200)
	db := &poolDBStub{pool: Pool{ID: 1, Name: "tm-pool-old", Platform: "openai", Sub2APIGroupID: &groupID, SellMultiplier: 0.6, Enabled: true}}
	svc := &PoolService{DB: db, Prov: &poolGroupProvisionerStub{}}
	_, err := svc.Update(context.Background(), 1, PoolInput{Name: "gpt-pool"})
	if httpStatus(err) != 400 {
		t.Fatalf("Update(invalid name) error = %v, want 400", err)
	}
}

func httpStatus(err error) int {
	var he *httpx.Error
	if errors.As(err, &he) {
		return he.Status
	}
	return 0
}

func TestPoolDeleteRequiresDisabled(t *testing.T) {
	groupID := int64(200)
	db := &poolDBStub{pool: Pool{ID: 1, Name: "tm-pool-test", Platform: "openai", Sub2APIGroupID: &groupID, SellMultiplier: 0.6, Enabled: true}}
	svc := &PoolService{DB: db, Prov: &poolGroupProvisionerStub{}}

	_, err := svc.Delete(context.Background(), 1)
	if httpStatus(err) != 409 {
		t.Fatalf("Delete(enabled) error = %v, want 409", err)
	}
	if db.deleted || len(db.opTypes) != 0 {
		t.Fatalf("enabled pool must stay untouched: deleted=%v ops=%v", db.deleted, db.opTypes)
	}
}

func TestPoolDeleteSoftDeletesAndQueuesGroupDelete(t *testing.T) {
	groupID := int64(200)
	db := &poolDBStub{pool: Pool{ID: 1, Name: "tm-pool-test", Platform: "openai", Sub2APIGroupID: &groupID, SellMultiplier: 0.6, MarketPublished: true}}
	svc := &PoolService{DB: db, Prov: &poolGroupProvisionerStub{}}

	if _, err := svc.Delete(context.Background(), 1); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if !db.deleted || db.pool.MarketPublished {
		t.Fatalf("pool should be soft-deleted and unpublished: deleted=%v published=%v", db.deleted, db.pool.MarketPublished)
	}
	if len(db.opTypes) != 1 || db.opTypes[0] != opshandlers.TypeGroupDelete {
		t.Fatalf("queued ops = %v, want [%s]", db.opTypes, opshandlers.TypeGroupDelete)
	}
	if got := db.updatePayload[0].GroupID; got != groupID {
		t.Errorf("group.delete group_id = %d, want %d", got, groupID)
	}

	// 删除后不再可见：再删返回 404，编辑与同步同样找不到
	if _, err := svc.Delete(context.Background(), 1); httpStatus(err) != 404 {
		t.Fatalf("second Delete() error = %v, want 404", err)
	}
	if _, err := svc.Update(context.Background(), 1, PoolInput{SellMultiplier: f64(0.7)}); httpStatus(err) != 404 {
		t.Fatalf("Update(deleted) error = %v, want 404", err)
	}
}

func TestPoolDeleteWithoutEngineGroupQueuesNothing(t *testing.T) {
	db := &poolDBStub{pool: Pool{ID: 1, Name: "tm-pool-test", Platform: "openai", SellMultiplier: 0.6}}
	svc := &PoolService{DB: db, Prov: &poolGroupProvisionerStub{}}

	if _, err := svc.Delete(context.Background(), 1); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if !db.deleted || len(db.opTypes) != 0 {
		t.Fatalf("deleted=%v ops=%v, want soft delete without sub2api op", db.deleted, db.opTypes)
	}
}

func TestPoolCreateWaitsForSameNameGroupDeletion(t *testing.T) {
	db := &poolDBStub{pool: Pool{ID: 2, Name: "tm-pool-test", Platform: "openai", SellMultiplier: 0.6}, groupDeleting: true}
	prov := &poolGroupProvisionerStub{}
	svc := &PoolService{DB: db, Prov: prov}

	_, err := svc.Create(context.Background(), PoolInput{Name: "tm-pool-test", Platform: "openai", SellMultiplier: f64(0.6)})
	if httpStatus(err) != 409 {
		t.Fatalf("Create() error = %v, want 409 while old group is being deleted", err)
	}
	if len(prov.specs) != 0 {
		t.Fatalf("must not look up / create sub2api group yet: %+v", prov.specs)
	}
}
