//go:build integration

package listing

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/yx-token/idlicat/market/internal/inspection"
	platformdb "github.com/yx-token/idlicat/market/internal/platform/db"
	"github.com/yx-token/idlicat/market/internal/testutil"
	"github.com/yx-token/idlicat/market/migrations"
)

func TestDetectionViewsRequireCompletedRunAndCurrentDefinition(t *testing.T) {
	_, db := testutil.NewDatabase(t)
	ctx := context.Background()
	if _, err := platformdb.Migrate(ctx, db, migrations.FS); err != nil {
		t.Fatal(err)
	}
	var sellerID, upstreamID, runID, verdictID int64
	if err := db.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('evidence@market.test','test') RETURNING id`).Scan(&sellerID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `INSERT INTO upstreams(seller_id,platform,auth_type,status,model_whitelist) VALUES($1,'openai','oauth','ready',ARRAY['gpt-6-astra','new-model']) RETURNING id`, sellerID).Scan(&upstreamID); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Add(-time.Second)
	checks := map[string]inspection.Check{"D14": {Status: "pass", CheckedAt: at, Samples: inspection.Samples{Observed: 20}, Metrics: map[string]inspection.Metric{"models": {Models: []inspection.ModelCheckResult{{Model: "gpt-6-astra", Status: "pass", Samples: 20}}}}}}
	encode := func(value any) []byte {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	settings := inspection.Defaults()
	snapshot := encode(map[string]any{"inspection_settings": settings})
	if err := db.QueryRow(ctx, `INSERT INTO probe_runs(upstream_id,batch_id,trigger,policy_version,model,status,finished_at,execution_snapshot) VALUES($1,'evidence','admin',1,'gpt-6-astra','done',now(),$2) RETURNING id`, upstreamID, snapshot).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `INSERT INTO quality_verdicts(upstream_id,run_id,model,verdict,checks) VALUES($1,$2,'gpt-6-astra','normal',$3) RETURNING id`, upstreamID, runID, encode(checks)).Scan(&verdictID); err != nil {
		t.Fatal(err)
	}
	assert := func(tested bool) {
		t.Helper()
		views, err := LoadInspectionViews(ctx, db, []int64{upstreamID})
		if err != nil {
			t.Fatal(err)
		}
		view := views[upstreamID]
		if len(view.ModelChecks) != 2 || view.ModelChecks[1].DetectionStatus != "untested" {
			t.Fatalf("unmeasured model must remain untested: %+v", view)
		}
		if tested {
			if view.DetectionStatus != "tested" || view.ModelChecks[0].DetectionStatus != "tested" || view.ModelChecks[0].DegradationStatus != "passed" {
				t.Fatalf("valid run evidence must be consistent: %+v", view)
			}
		} else if view.DetectionStatus != "untested" || view.ModelChecks[0].DetectionStatus != "untested" || view.ModelChecks[0].DegradationStatus != "untested" {
			t.Fatalf("invalid run evidence must never appear passed: %+v", view)
		}
	}
	assert(true)
	if _, err := db.Exec(ctx, `UPDATE quality_verdicts SET run_id=NULL WHERE id=$1`, verdictID); err != nil {
		t.Fatal(err)
	}
	assert(false)
	if _, err := db.Exec(ctx, `UPDATE quality_verdicts SET run_id=$2 WHERE id=$1`, verdictID, runID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE probe_runs SET status='failed' WHERE id=$1`, runID); err != nil {
		t.Fatal(err)
	}
	assert(false)
	if _, err := db.Exec(ctx, `UPDATE probe_runs SET status='done',execution_snapshot='{}' WHERE id=$1`, runID); err != nil {
		t.Fatal(err)
	}
	assert(false)
	oldSettings := inspection.Defaults()
	oldSettings.CheckOverrides["D14"] = inspection.CheckOverride{Calibrated: true, CalibrationNote: "expired definition"}
	if _, err := db.Exec(ctx, `UPDATE probe_runs SET execution_snapshot=$2 WHERE id=$1`, runID, encode(map[string]any{"inspection_settings": oldSettings})); err != nil {
		t.Fatal(err)
	}
	assert(false)
	if _, err := db.Exec(ctx, `UPDATE probe_runs SET execution_snapshot=$2 WHERE id=$1`, runID, snapshot); err != nil {
		t.Fatal(err)
	}
	assert(true)
	if _, err := db.Exec(ctx, `INSERT INTO upstream_quality_state(upstream_id,deductions_reset_at) VALUES($1,now())`, upstreamID); err != nil {
		t.Fatal(err)
	}
	assert(false)
}
