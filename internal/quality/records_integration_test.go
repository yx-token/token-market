//go:build integration

package quality

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/DueGin/token-market/internal/listing"
	"github.com/DueGin/token-market/internal/testutil"
)

func TestRunSummaryPlannedStepsAndSellerProbeRuns(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, pool := testutil.NewMarketDatabase(t)
	var sellerID, otherSellerID, upstreamID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES ('records-owner@test','unused') RETURNING id`).Scan(&sellerID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES ('records-other@test','unused') RETURNING id`).Scan(&otherSellerID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO upstreams(seller_id,platform,auth_type,status) VALUES($1,'anthropic','oauth','ready') RETURNING id`, sellerID).Scan(&upstreamID); err != nil {
		t.Fatal(err)
	}
	var runningID, oldID int64
	if err := pool.QueryRow(ctx, `INSERT INTO probe_runs(upstream_id,batch_id,trigger,policy_version,model,status,execution_snapshot,queued_at)
		VALUES($1,'records-running','admin',1,'模型甲','running', $2, now()-interval '2 minutes') RETURNING id`, upstreamID,
		`{"instances":[{"steps":[{},{}]},{"steps":[{},{},{}]}]}`).Scan(&runningID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO probe_results(run_id,suite,passed) VALUES($1,'inspection_capability',true),($1,'inspection_capability',false)`, runningID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO probe_runs(upstream_id,batch_id,trigger,policy_version,model,status,queued_at)
		VALUES($1,'records-old','admin',1,'模型乙','done', now()) RETURNING id`, upstreamID).Scan(&oldID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO test_reports(upstream_id,run_id,trigger,verdict,summary,impact) VALUES($1,$2,'admin','normal','{}','{}')`, upstreamID, runningID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO third_party_checks(run_id,upstream_id,provider,model,format,config,status,deadline)
		VALUES($1,$2,'bazaarlink','身份模型','openai','{}','running',now()+interval '1 hour')`, runningID, upstreamID); err != nil {
		t.Fatal(err)
	}

	records, err := ListRunRecords(ctx, pool, RecordFilter{UpstreamID: upstreamID}, 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("检测记录数量错误：%+v", records)
	}
	plannedByID := map[int64]*int{}
	for _, record := range records {
		plannedByID[record.ID] = record.Planned
	}
	if plannedByID[runningID] == nil || *plannedByID[runningID] != 5 || plannedByID[oldID] != nil {
		t.Fatalf("计划题数错误：%+v", records)
	}

	sellerRuns, err := SellerProbeRuns(ctx, pool, sellerID, upstreamID, 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(sellerRuns) != 2 || sellerRuns[0].ID != runningID || sellerRuns[0].Progress.Answered != 2 || sellerRuns[0].Progress.Total == nil || *sellerRuns[0].Progress.Total != 5 {
		t.Fatalf("卖家记录排序或进度错误：%+v", sellerRuns)
	}
	if len(sellerRuns[0].ThirdParty) != 1 || sellerRuns[0].ThirdParty[0].Provider != "bazaarlink" || sellerRuns[0].ThirdParty[0].Status != "running" || sellerRuns[0].ReportID == nil || sellerRuns[0].Verdict == nil {
		t.Fatalf("第三方或报告关联错误：%+v", sellerRuns[0])
	}
	raw, _ := json.Marshal(sellerRuns)
	for _, key := range []string{"prompt", "excerpt", "expected", "grader", "instance_hash", "summary"} {
		if strings.Contains(string(raw), key) {
			t.Fatalf("卖家检测记录泄露内部字段 %q：%s", key, raw)
		}
	}
	if _, err := SellerProbeRuns(ctx, pool, otherSellerID, upstreamID, 20, 0); err == nil || err != listing.ErrUpstreamNotFound {
		t.Fatalf("他人上游应返回归属错误，得到 %v", err)
	}
	_ = oldID
}
