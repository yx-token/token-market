package quality

import (
	"context"
	"testing"

	"github.com/yx-token/token-market/market/internal/listing"
	platformdb "github.com/yx-token/token-market/market/internal/platform/db"
	"github.com/yx-token/token-market/market/internal/scheduling"
	"github.com/yx-token/token-market/market/internal/settings"
	"github.com/yx-token/token-market/market/internal/testutil"
	"github.com/yx-token/token-market/market/migrations"
)

func TestCodingOAuthPoliciesAndReferences(t *testing.T) {
	_, pool := testutil.NewDatabase(t)
	ctx := context.Background()
	if _, err := platformdb.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatal(err)
	}
	var sellerID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('coding-policy@test.local','test') RETURNING id`).Scan(&sellerID); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ platform, authType string }{{"kimi", "oauth"}, {"zhipu", "oauth"}, {"zhipu", "oauth-intl"}} {
		t.Run(test.platform+"/"+test.authType, func(t *testing.T) {
			equivalents := listing.AuthTypeEquivalents(test.platform, test.authType)
			codingAuth := equivalents[1]
			flows := policyFlowsFixture()
			flows.Identity.Source = "self"
			flows.Intelligence.Models = []IntelligenceModel{{Model: "coding-model"}}
			for _, authType := range equivalents {
				if _, err := pool.Exec(ctx, `INSERT INTO detection_policies(platform,auth_type,enabled,default_grade,flows)
					VALUES($1,$2,true,'B',$3) ON CONFLICT(platform,auth_type) DO UPDATE SET enabled=true,default_grade='B',flows=EXCLUDED.flows`, test.platform, authType, mustJSON(flows)); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := pool.Exec(ctx, `UPDATE detection_policies SET default_grade='A' WHERE platform=$1 AND auth_type=$2`, test.platform, codingAuth); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE detection_policies SET default_grade='S' WHERE platform=$1 AND auth_type=$2`, test.platform, test.authType); err != nil {
				t.Fatal(err)
			}
			var upstreamID, referenceID int64
			for index, id := range []*int64{&upstreamID, &referenceID} {
				authType := test.authType
				if index == 1 {
					authType = codingAuth
				}
				if err := pool.QueryRow(ctx, `INSERT INTO upstreams(seller_id,platform,auth_type,status,health,is_reference,model_whitelist)
					VALUES($1,$2,$3,'ready','healthy',$4,ARRAY['coding-model']) RETURNING id`, sellerID, test.platform, authType, index == 1).Scan(id); err != nil {
					t.Fatal(err)
				}
				if _, err := pool.Exec(ctx, `UPDATE upstreams SET sub2api_account_id=id WHERE id=$1`, *id); err != nil {
					t.Fatal(err)
				}
			}
			for _, wantGrade := range []string{"S", "A", "B"} {
				policy, err := EffectivePolicy(ctx, pool, upstreamID, test.platform, test.authType)
				if err != nil || !policy.Enabled || policy.DefaultGrade != wantGrade {
					t.Fatalf("policy priority want %s: %+v %v", wantGrade, policy, err)
				}
				_, upstreams, err := scheduling.LoadPlanInputs(ctx, pool, settings.Defaults())
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, upstream := range upstreams {
					if upstream.ID == upstreamID {
						found = true
						if upstream.DefaultGrade != wantGrade {
							t.Fatalf("scheduler grade want %s, got %s", wantGrade, upstream.DefaultGrade)
						}
					}
				}
				if !found {
					t.Fatal("OAuth upstream missing from scheduling inputs")
				}
				if wantGrade != "B" {
					removeAuth := test.authType
					if wantGrade == "A" {
						removeAuth = codingAuth
					}
					if _, err := pool.Exec(ctx, `DELETE FROM detection_policies WHERE platform=$1 AND auth_type=$2`, test.platform, removeAuth); err != nil {
						t.Fatal(err)
					}
				}
			}
			if _, err := pool.Exec(ctx, `INSERT INTO detection_policies(platform,auth_type,enabled,default_grade,flows) VALUES($1,$2,true,'A',$3)`, test.platform, codingAuth, mustJSON(flows)); err != nil {
				t.Fatal(err)
			}
			engine := &Engine{DB: pool}
			runID, err := engine.QueueRun(ctx, upstreamID, "onboarding", "intelligence")
			if err != nil {
				t.Fatal(err)
			}
			var actualReferenceID int64
			if err := pool.QueryRow(ctx, `SELECT ref.upstream_id FROM probe_runs ref JOIN probe_runs target ON ref.batch_id=target.batch_id WHERE target.id=$1 AND ref.trigger='reference'`, runID).Scan(&actualReferenceID); err != nil || actualReferenceID != referenceID {
				t.Fatalf("OAuth failed to reuse Coding Plan reference: %d %v", actualReferenceID, err)
			}
			upstream, err := engine.loadUpstream(ctx, upstreamID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE probe_runs SET execution_snapshot=jsonb_set(execution_snapshot,'{instances}','[]'::jsonb) WHERE id=$1`, runID); err != nil {
				t.Fatal(err)
			}
			if _, err := engine.attributeInspection(ctx, runRow{id: runID, model: "coding-model"}, upstream, Assessment{}); err != nil {
				t.Fatalf("equivalent reference history query failed: %v", err)
			}
			policy, err := UpsertPolicy(ctx, pool, test.platform, test.authType, PolicyInput{}, sellerID)
			if err != nil || policy.DefaultGrade != "A" || policy.AuthType != test.authType {
				t.Fatalf("new OAuth policy did not inherit Coding Plan template: %+v %v", policy, err)
			}
		})
	}
}
