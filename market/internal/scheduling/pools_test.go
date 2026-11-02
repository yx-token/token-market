package scheduling

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
	updatePayload []opshandlers.GroupUpdatePayload
}

func (db *poolDBStub) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	if strings.Contains(query, "UPDATE pools SET sub2api_group_id") {
		groupID := args[1].(int64)
		db.pool.Sub2APIGroupID = &groupID
	}
	return pgconn.CommandTag{}, nil
}

func (db *poolDBStub) Query(_ context.Context, query string, _ ...any) (pgx.Rows, error) {
	return nil, fmt.Errorf("unexpected query: %s", query)
}

func (db *poolDBStub) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	switch {
	case strings.Contains(query, "INSERT INTO pools"):
		return poolRowStub{scan: func(dest ...any) error {
			*dest[0].(*int64) = db.pool.ID
			return nil
		}}
	case strings.Contains(query, "FROM pools WHERE id = $1"):
		return poolRowStub{scan: db.scanPool}
	case strings.Contains(query, "INSERT INTO sub2api_ops"):
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
		})
	}
}
