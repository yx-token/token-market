//go:build integration

package quality

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/yx-token/token-market/market/internal/edgecfg"
	"github.com/yx-token/token-market/market/internal/settings"
)

func thirdPartyEngineFixture(t *testing.T, handler http.HandlerFunc) (*Engine, runRow, *upstreamRow, *Policy) {
	t.Helper()
	pool, run, ref := inspectionDB(t)
	ctx := context.Background()
	run.flows, run.identitySource = []string{"identity"}, "third_party"
	run.models, run.degradationModels = []string{}, []string{}
	flows := policyFlowsFixture()
	flows.Intelligence.Enabled, flows.Degradation.Enabled = false, false
	sites := []ThirdPartyCheck{{Provider: "bazaarlink", Model: run.model, OnMismatch: "degraded", OnSubmodelMismatch: "suspected"}}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	redisServer := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	e := &Engine{DB: pool, Keyring: testInspectionKeyring(t), Edge: edgecfg.Store{Redis: rdb}, EdgePublicURL: "https://edge.test",
		ThirdPartyHTTP: srv.Client(), Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Settings: func(context.Context) (settings.Settings, error) { return settings.Defaults(), nil },
		Wait:     func(ctx context.Context, _ time.Duration) error { return ctx.Err() }}
	enc, err := e.Keyring.SealString("local-probe-key", "system-key:101")
	if err != nil {
		t.Fatal(err)
	}
	queries := []struct {
		sql  string
		args []any
	}{
		{`DELETE FROM probe_runs WHERE id=$1`, []any{ref.id}},
		{`UPDATE upstreams SET probe_group_id=101,status='ready',health='healthy',grade='B' WHERE id=$1`, []any{run.upstreamID}},
		{`INSERT INTO system_keys(purpose,sub2api_group_id,key_enc,sub2api_key_id) VALUES('probe',101,$1,101)`, []any{enc}},
		{`UPDATE detection_policies SET enabled=true,auto_pause=false,flows=$1,third_party=$2 WHERE platform='anthropic'`, []any{mustJSON(flows), mustJSON(sites)}},
		{`UPDATE detection_providers SET enabled=true,base_url=$1,max_runs_per_hour=10 WHERE name='bazaarlink'`, []any{srv.URL}},
		{`UPDATE probe_runs SET flows=ARRAY['identity'],identity_source='third_party',models='{}',degradation_models='{}',status='running' WHERE id=$1`, []any{run.id}},
	}
	for _, q := range queries {
		if _, err := pool.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	u, err := e.loadUpstream(ctx, run.upstreamID)
	if err != nil {
		t.Fatal(err)
	}
	p, err := EffectivePolicy(ctx, pool, u.id, u.platform, u.authType)
	if err != nil {
		t.Fatal(err)
	}
	return e, run, u, p
}

func TestThirdPartyPureIdentitySubmitsPollsAndPauses(t *testing.T) {
	var submits, polls atomic.Int32
	e, fixtureRun, u, _ := thirdPartyEngineFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			submits.Add(1)
			_, _ = io.WriteString(w, `{"runId":"identity-mismatch"}`)
			return
		}
		polls.Add(1)
		_, _ = io.WriteString(w, `{"status":"completed","identityAssessment":{"verdict":{"status":"plain_mismatch","trueModel":"gpt-4o"}}}`)
	})
	ctx := context.Background()
	// 从 QueueRun 开始验证冻结的流程、来源与模型，随后执行纯第三方快路径。
	if _, err := e.DB.Exec(ctx, `UPDATE probe_runs SET status='done' WHERE id=$1`, fixtureRun.id); err != nil {
		t.Fatal(err)
	}
	id, err := e.QueueRun(ctx, u.id, "admin", "identity")
	if err != nil {
		t.Fatal(err)
	}
	run := fixtureRun
	run.id = id
	if err := e.DB.QueryRow(ctx, `UPDATE probe_runs SET status='running' WHERE id=$1 RETURNING flows,identity_source,model,models,degradation_models`, id).
		Scan(&run.flows, &run.identitySource, &run.model, &run.models, &run.degradationModels); err != nil {
		t.Fatal(err)
	}
	if run.identitySource != "third_party" || len(run.flows) != 1 || run.flows[0] != "identity" || run.model == "" {
		t.Fatalf("incorrect queued third-party flow: %+v", run)
	}
	if err := e.executeTarget(ctx, run, nil, time.Second); err != nil {
		t.Fatal(err)
	}
	var hash string
	if err := e.DB.QueryRow(ctx, `UPDATE third_party_checks SET next_attempt_at=now() WHERE run_id=$1 RETURNING token_hash`, id).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := e.advanceThirdParty(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var status, verdict, identity, health string
	var paused, revoked bool
	var internal, checks int
	if err := e.DB.QueryRow(ctx, `SELECT r.status,r.summary->>'verdict',r.summary->'identity'->>'status',u.health,
		(tr.impact->>'buyer_facing_paused')::bool,c.token_revoked,
		(SELECT count(*) FROM probe_results WHERE run_id=r.id),(SELECT count(*) FROM third_party_checks WHERE run_id=r.id)
		FROM probe_runs r JOIN upstreams u ON u.id=r.upstream_id JOIN test_reports tr ON tr.run_id=r.id
		JOIN third_party_checks c ON c.run_id=r.id WHERE r.id=$1`, id).
		Scan(&status, &verdict, &identity, &health, &paused, &revoked, &internal, &checks); err != nil {
		t.Fatal(err)
	}
	if status != "done" || verdict != "degraded" || identity != "fail" || health != "degraded" || !paused || !revoked || internal != 0 || checks != 1 || submits.Load() != 1 || polls.Load() != 1 {
		t.Fatalf("third-party chain: %s %s %s %s paused=%t revoked=%t probes=%d checks=%d submits=%d polls=%d", status, verdict, identity, health, paused, revoked, internal, checks, submits.Load(), polls.Load())
	}
	if _, err := e.Edge.Get(ctx, hash); !errors.Is(err, edgecfg.ErrNotFound) {
		t.Fatalf("temporary token not revoked: %v", err)
	}
}

func TestThirdPartySkippedAndMissingModelAreVisible(t *testing.T) {
	for _, condition := range []string{"disabled", "missing_provider", "missing_model", "no_sites"} {
		t.Run(condition, func(t *testing.T) {
			e, run, u, p := thirdPartyEngineFixture(t, func(w http.ResponseWriter, _ *http.Request) {
				t.Error("invalid configuration must not submit")
				w.WriteHeader(500)
			})
			ctx := context.Background()
			wantStatus, wantError := "skipped", "disabled"
			switch condition {
			case "disabled":
				_, _ = e.DB.Exec(ctx, `UPDATE detection_providers SET enabled=false`)
			case "missing_provider":
				_, _ = e.DB.Exec(ctx, `DELETE FROM detection_providers`)
			case "missing_model":
				run.model, u.modelWhitelist, p.ThirdParty[0].Model = "", nil, ""
				wantStatus, wantError = "failed", "requires a model"
			case "no_sites":
				p.ThirdParty = nil
				wantError = "no third-party detection providers configured"
			}
			if err := e.DB.QueryRow(ctx, `UPDATE probe_runs SET trigger='onboarding',execution_snapshot=$2 WHERE id=$1 RETURNING id`, run.id, mustJSON(executionSnapshot{Policy: p})).Scan(&run.id); err != nil {
				t.Fatal(err)
			}
			run.trigger = "onboarding"
			if err := e.submitThirdParty(ctx, run, u, p); err != nil {
				t.Fatal(err)
			}
			st := defaultInspectionSettings()
			if waiting, err := e.startThirdParty(ctx, run, u, p, Assessment{Verdict: "inconclusive", InspectionSettings: &st}); err != nil || !waiting {
				t.Fatalf("start: waiting=%t %v", waiting, err)
			}
			if err := e.advanceThirdParty(ctx); err != nil {
				t.Fatal(err)
			}
			var status, identity, summary string
			if err := e.DB.QueryRow(ctx, `SELECT status,summary->'identity'->>'status',summary::text FROM probe_runs WHERE id=$1`, run.id).Scan(&status, &identity, &summary); err != nil {
				t.Fatal(err)
			}
			if status != "failed" || identity != "inconclusive" || !strings.Contains(summary, wantError) || !strings.Contains(summary, wantStatus) {
				t.Fatalf("configuration failure hidden: %s %s %s", status, identity, summary)
			}
		})
	}
}

func TestThirdPartySubmissionRetryAndDeadline(t *testing.T) {
	for _, limited := range []bool{true, false} {
		t.Run(fmt.Sprintf("rate_limited_%t", limited), func(t *testing.T) {
			var calls atomic.Int32
			e, run, u, p := thirdPartyEngineFixture(t, func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				if limited {
					w.Header().Set("Retry-After", "3600")
					w.WriteHeader(429)
					return
				}
				w.WriteHeader(503)
			})
			ctx := context.Background()
			for i := 0; i < 2; i++ {
				if err := e.submitThirdParty(ctx, run, u, p); err != nil {
					t.Fatal(err)
				}
			}
			var id int64
			var attempts int
			var revoked, delayed bool
			if err := e.DB.QueryRow(ctx, `SELECT id,attempts,token_revoked,next_attempt_at>now() FROM third_party_checks WHERE run_id=$1`, run.id).Scan(&id, &attempts, &revoked, &delayed); err != nil {
				t.Fatal(err)
			}
			wantAttempts := 1
			if limited {
				wantAttempts = 0
			}
			if calls.Load() != 1 || attempts != wantAttempts || !revoked || !delayed {
				t.Fatalf("resume bypassed retry: calls=%d attempts=%d revoked=%t delayed=%t", calls.Load(), attempts, revoked, delayed)
			}
			if !limited {
				for i := 0; i < 2; i++ {
					if _, err := e.DB.Exec(ctx, `UPDATE third_party_checks SET next_attempt_at=now() WHERE id=$1`, id); err != nil {
						t.Fatal(err)
					}
					if err := e.advanceThirdParty(ctx); err != nil {
						t.Fatal(err)
					}
				}
				var status string
				if err := e.DB.QueryRow(ctx, `SELECT status,attempts FROM third_party_checks WHERE id=$1`, id).Scan(&status, &attempts); err != nil || status != "failed" || attempts != 3 || calls.Load() != 3 {
					t.Fatalf("retry limit: %s %d calls=%d %v", status, attempts, calls.Load(), err)
				}
				return
			}
			if _, err := e.DB.Exec(ctx, `UPDATE third_party_checks SET deadline=now()-interval '1 second' WHERE id=$1`, id); err != nil {
				t.Fatal(err)
			}
			if err := e.advanceThirdParty(ctx); err != nil {
				t.Fatal(err)
			}
			var status string
			if err := e.DB.QueryRow(ctx, `SELECT status FROM third_party_checks WHERE id=$1`, id).Scan(&status); err != nil || status != "timeout" || calls.Load() != 1 {
				t.Fatalf("deadline did not stop submission: %s calls=%d %v", status, calls.Load(), err)
			}
		})
	}
}

func TestThirdPartyConcurrentHourlyLimit(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	e, run, u, p := thirdPartyEngineFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
		_, _ = io.WriteString(w, `{"runId":"limited-run"}`)
	})
	ctx := context.Background()
	if _, err := e.DB.Exec(ctx, `UPDATE detection_providers SET max_runs_per_hour=1`); err != nil {
		t.Fatal(err)
	}
	var secondRun int64
	if err := e.DB.QueryRow(ctx, `INSERT INTO probe_runs(upstream_id,batch_id,trigger,policy_version,model,mode) VALUES($1,'second-third-party','admin',1,$2,'full') RETURNING id`, u.id, run.model).Scan(&secondRun); err != nil {
		t.Fatal(err)
	}
	var secondCheck int64
	if err := e.DB.QueryRow(ctx, `INSERT INTO third_party_checks(run_id,upstream_id,provider,model,format,config,deadline)
		VALUES($1,$2,'bazaarlink',$3,'anthropic',$4,now()+interval '1 hour') RETURNING id`, secondRun, u.id, run.model, mustJSON(p.ThirdParty[0])).Scan(&secondCheck); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- e.submitThirdParty(ctx, run, u, p) }()
	defer close(release)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first submission did not enter provider")
	}
	if err := e.submitCheck(ctx, tpCheckRow{id: secondCheck, upstreamID: u.id, provider: "bazaarlink", model: run.model}); err != nil {
		t.Fatal(err)
	}
	// 放行第一次提交后，第二条仍须被同一小时的额度阻挡。
	release <- struct{}{}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := e.submitCheck(ctx, tpCheckRow{id: secondCheck, upstreamID: u.id, provider: "bazaarlink", model: run.model}); err != nil {
		t.Fatal(err)
	}
	var status, reason string
	var delayed bool
	if err := e.DB.QueryRow(ctx, `SELECT status,COALESCE(error,''),next_attempt_at>now() FROM third_party_checks WHERE id=$1`, secondCheck).Scan(&status, &reason, &delayed); err != nil || calls.Load() != 1 || status != "pending" || !delayed || !strings.Contains(reason, "hourly") {
		t.Fatalf("concurrent hourly limit bypassed: calls=%d status=%s delayed=%t reason=%s %v", calls.Load(), status, delayed, reason, err)
	}
}

func TestThirdPartyPollRateLimitAndTimeoutConcludeRun(t *testing.T) {
	var submits, polls atomic.Int32
	e, run, _, _ := thirdPartyEngineFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			submits.Add(1)
			_, _ = io.WriteString(w, `{"runId":"waiting-run"}`)
			return
		}
		polls.Add(1)
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(429)
	})
	ctx := context.Background()
	if err := e.executeTarget(ctx, run, nil, time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := e.DB.Exec(ctx, `UPDATE third_party_checks SET next_attempt_at=now(),attempts=29 WHERE run_id=$1`, run.id); err != nil {
		t.Fatal(err)
	}
	if err := e.advanceThirdParty(ctx); err != nil {
		t.Fatal(err)
	}
	var status string
	var attempts int
	var delayed bool
	if err := e.DB.QueryRow(ctx, `SELECT status,attempts,next_attempt_at>now()+interval '59 minutes' FROM third_party_checks WHERE run_id=$1`, run.id).
		Scan(&status, &attempts, &delayed); err != nil || status != "running" || attempts != 29 || !delayed {
		t.Fatalf("poll rate limit should wait without exhausting attempts: %s %d delayed=%t %v", status, attempts, delayed, err)
	}
	if _, err := e.DB.Exec(ctx, `UPDATE third_party_checks SET deadline=now()-interval '1 second' WHERE run_id=$1`, run.id); err != nil {
		t.Fatal(err)
	}
	if err := e.advanceThirdParty(ctx); err != nil {
		t.Fatal(err)
	}
	var checkStatus, verdict, identity string
	var revoked bool
	if err := e.DB.QueryRow(ctx, `SELECT r.status,c.status,c.token_revoked,r.summary->>'verdict',r.summary->'identity'->>'status'
		FROM probe_runs r JOIN third_party_checks c ON c.run_id=r.id WHERE r.id=$1`, run.id).
		Scan(&status, &checkStatus, &revoked, &verdict, &identity); err != nil || status != "done" || checkStatus != "timeout" || !revoked || verdict != "inconclusive" || identity != "inconclusive" || submits.Load() != 1 || polls.Load() != 1 {
		t.Fatalf("waiting run did not time out and conclude: %s %s revoked=%t %s %s submits=%d polls=%d %v", status, checkStatus, revoked, verdict, identity, submits.Load(), polls.Load(), err)
	}
}

func TestThirdPartyFailedTokenRevocationRemainsRetryable(t *testing.T) {
	redisServer := miniredis.RunT(t)
	var calls atomic.Int32
	e, run, u, p := thirdPartyEngineFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			redisServer.SetError("ERR temporary redis outage")
			w.Header().Set("Retry-After", "3600")
			w.WriteHeader(429)
			return
		}
		_, _ = io.WriteString(w, `{"runId":"retry-run"}`)
	})
	rdb := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	e.Edge.Redis = rdb
	ctx := context.Background()
	if err := e.submitThirdParty(ctx, run, u, p); err != nil {
		t.Fatal(err)
	}
	var oldHash string
	var revoked bool
	if err := e.DB.QueryRow(ctx, `SELECT token_hash,token_revoked FROM third_party_checks WHERE run_id=$1`, run.id).Scan(&oldHash, &revoked); err != nil || revoked {
		t.Fatalf("failed deletion must remain tracked: revoked=%t %v", revoked, err)
	}
	redisServer.SetError("")
	if _, err := e.Edge.Get(ctx, oldHash); err != nil {
		t.Fatal(err)
	}
	if _, err := e.DB.Exec(ctx, `UPDATE third_party_checks SET next_attempt_at=now() WHERE run_id=$1`, run.id); err != nil {
		t.Fatal(err)
	}
	if err := e.submitThirdParty(ctx, run, u, p); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Edge.Get(ctx, oldHash); !errors.Is(err, edgecfg.ErrNotFound) {
		t.Fatalf("old token leaked across retry: %v", err)
	}
	var status string
	if err := e.DB.QueryRow(ctx, `SELECT status FROM third_party_checks WHERE run_id=$1`, run.id).Scan(&status); err != nil || status != "running" || calls.Load() != 2 {
		t.Fatalf("retry failed after redis recovered: %s calls=%d %v", status, calls.Load(), err)
	}
}

func TestThirdPartyQueueWithoutModelsRecordsFailure(t *testing.T) {
	e, fixtureRun, u, p := thirdPartyEngineFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("empty model must not reach provider")
		w.WriteHeader(400)
	})
	ctx := context.Background()
	p.Flows.Intelligence.Models, p.Flows.Degradation.Models = []IntelligenceModel{}, []IntelligenceModel{}
	p.ThirdParty[0].Model = ""
	if _, err := e.DB.Exec(ctx, `UPDATE detection_policies SET flows=$1,models='{}',third_party=$2 WHERE platform='anthropic'`, mustJSON(p.Flows), mustJSON(p.ThirdParty)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.DB.Exec(ctx, `UPDATE probe_runs SET status='done' WHERE id=$1`, fixtureRun.id); err != nil {
		t.Fatal(err)
	}
	id, err := e.QueueRun(ctx, u.id, "admin", "identity")
	if err != nil {
		t.Fatal(err)
	}
	run := fixtureRun
	run.id = id
	if err := e.DB.QueryRow(ctx, `UPDATE probe_runs SET status='running' WHERE id=$1 RETURNING model`, id).Scan(&run.model); err != nil || run.model != "" {
		t.Fatalf("expected queued empty model: %q %v", run.model, err)
	}
	if err := e.executeTarget(ctx, run, nil, time.Second); err != nil {
		t.Fatal(err)
	}
	if err := e.advanceThirdParty(ctx); err != nil {
		t.Fatal(err)
	}
	var status, reason, identity string
	if err := e.DB.QueryRow(ctx, `SELECT c.status,c.error,r.summary->'identity'->>'status' FROM third_party_checks c
		JOIN probe_runs r ON r.id=c.run_id WHERE r.id=$1`, id).Scan(&status, &reason, &identity); err != nil || status != "failed" || !strings.Contains(reason, "requires a model") || identity != "inconclusive" {
		t.Fatalf("empty-model run silently skipped: %s %s %s %v", status, reason, identity, err)
	}
}
