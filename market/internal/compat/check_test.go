package compat

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/yx-token/token-market/market/contract"
)

const testContract = `
version: 1
tables:
  usage_logs:
    large: true
    used_by: [billing]
    columns: [id, request_id, total_cost, model]
  accounts:
    used_by: [scheduling]
    columns: [id, name, platform, priority]
enums:
  accounts.platform: [anthropic, openai, kimi]
`

func spec(t *testing.T) *contract.Spec {
	t.Helper()
	s, err := contract.Parse([]byte(testContract))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func rulesOf(fs []Finding) map[string]Finding {
	out := map[string]Finding{}
	for _, f := range fs {
		out[f.Rule+":"+f.Table+"."+f.Column] = f
	}
	return out
}

func TestAnalyzeDetectsContractBreakingChanges(t *testing.T) {
	sql := `
ALTER TABLE usage_logs DROP COLUMN IF EXISTS total_cost, ADD COLUMN extra_cost numeric;
ALTER TABLE public.usage_logs RENAME COLUMN request_id TO req_id;
ALTER TABLE ONLY accounts ALTER COLUMN priority TYPE bigint;
ALTER TABLE accounts ALTER COLUMN name DROP NOT NULL;
ALTER TABLE accounts RENAME TO accounts_v2;
DROP TABLE IF EXISTS foo, usage_logs CASCADE;
`
	got := rulesOf(AnalyzeMigration(spec(t), "999_test.sql", sql))
	want := map[string]Severity{
		"drop_column:usage_logs.total_cost":   SeverityConflict,
		"add_column:usage_logs.extra_cost":    SeverityInfo,
		"rename_column:usage_logs.request_id": SeverityConflict,
		"alter_column_type:accounts.priority": SeverityConflict,
		"drop_not_null:accounts.name":         SeverityWarning,
		"rename_table:accounts.":              SeverityConflict,
		"drop_table:usage_logs.":              SeverityConflict,
	}
	for key, sev := range want {
		f, ok := got[key]
		if !ok {
			t.Errorf("missing finding %s; got %v", key, keys(got))
			continue
		}
		if f.Severity != sev {
			t.Errorf("%s severity = %s, want %s", key, f.Severity, sev)
		}
	}
	if f := got["rename_column:usage_logs.request_id"]; !strings.Contains(f.Detail, "req_id") || f.Line != 3 {
		t.Errorf("rename detail/line wrong: %+v", f)
	}
	if f := got["drop_column:usage_logs.total_cost"]; len(f.UsedBy) != 1 || f.UsedBy[0] != "billing" {
		t.Errorf("used_by not propagated: %+v", f)
	}
}

func TestAnalyzeIgnoresNonContractChanges(t *testing.T) {
	sql := `
ALTER TABLE payment_orders DROP COLUMN bonus_amount;
ALTER TABLE usage_logs DROP COLUMN some_unused_column;
ALTER TABLE accounts DROP CONSTRAINT accounts_platform_check;
ALTER TABLE accounts ALTER COLUMN priority SET DEFAULT 10;
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_x ON usage_logs (model);
`
	if fs := AnalyzeMigration(spec(t), "x.sql", sql); len(fs) != 0 {
		t.Fatalf("expected no findings, got %+v", fs)
	}
}

func TestAnalyzeEnumConstraints(t *testing.T) {
	sql := `
ALTER TABLE accounts ADD CONSTRAINT accounts_platform_check
    CHECK (platform IN ('anthropic', 'openai', 'typesafe'));
ALTER TABLE accounts ADD CONSTRAINT accounts_platform_check2
    CHECK ((platform)::text = ANY (ARRAY['anthropic'::text, 'openai'::text, 'kimi'::text, 'deepseek'::text]));
`
	fs := AnalyzeMigration(spec(t), "x.sql", sql)
	got := rulesOf(fs)
	removed, ok := got["enum_value_removed:accounts.platform"]
	if !ok || !strings.Contains(removed.Detail, "kimi") {
		t.Fatalf("expected kimi removal conflict, got %+v", fs)
	}
	var added []string
	for _, f := range fs {
		if f.Rule == "enum_value_added" {
			added = append(added, f.Detail)
		}
	}
	if len(added) != 2 || !strings.Contains(added[0], "typesafe") || !strings.Contains(added[1], "deepseek") {
		t.Fatalf("added values = %v", added)
	}
}

func TestAnalyzeLargeTableOperations(t *testing.T) {
	sql := `
CREATE INDEX idx_usage_model ON usage_logs (model);
UPDATE usage_logs SET model = lower(model);
DELETE FROM ONLY usage_logs WHERE id < 10;
DO $$ BEGIN UPDATE accounts SET priority = 1; END $$;
`
	got := rulesOf(AnalyzeMigration(spec(t), "x.sql", sql))
	for _, key := range []string{"lock_large_table:usage_logs.", "long_running_dml:usage_logs.", "procedural_manual_review:accounts."} {
		if f, ok := got[key]; !ok || f.Severity != SeverityWarning {
			t.Errorf("missing warning %s; got %v", key, keys(got))
		}
	}
}

func TestCheckStaticComputesPendingAndModified(t *testing.T) {
	current := Applied{Desc: "current", Checksums: map[string]string{
		"001_init.sql":    Checksum([]byte("CREATE TABLE accounts(id int);")),
		"002_old.sql":     Checksum([]byte("SELECT 1;")),
		"003_removed.sql": Checksum([]byte("SELECT 2;")),
	}}
	target := &MigrationSet{Desc: "target", Names: []string{"001_init.sql", "002_old.sql", "004_new.sql"}, Contents: map[string][]byte{
		"001_init.sql": []byte("CREATE TABLE accounts(id int);"),
		"002_old.sql":  []byte("SELECT 1; -- edited"),
		"004_new.sql":  []byte("ALTER TABLE accounts DROP COLUMN platform;"),
	}}
	r := CheckStatic(context.Background(), spec(t), current, target)
	if len(r.Pending) != 1 || r.Pending[0] != "004_new.sql" {
		t.Fatalf("pending = %v", r.Pending)
	}
	got := rulesOf(r.Findings)
	if _, ok := got["drop_column:accounts.platform"]; !ok {
		t.Errorf("missing drop_column conflict: %v", keys(got))
	}
	if _, ok := got["migration_modified:."]; !ok {
		t.Errorf("missing migration_modified: %v", keys(got))
	}
	if _, ok := got["migration_removed:."]; !ok {
		t.Errorf("missing migration_removed: %v", keys(got))
	}
	if !r.Failed(SeverityConflict) {
		t.Error("report with conflict should fail")
	}
	if r.Findings[0].Severity != SeverityConflict {
		t.Errorf("findings should be sorted by severity, first = %+v", r.Findings[0])
	}
	if md := r.Markdown(); !strings.Contains(md, "冲突 1") {
		t.Errorf("markdown summary missing: %s", md)
	}
}

// 使用本地 sub2api 仓库做端到端验证：设置 TM_SUB2API_REPO 指向 sub2api 仓库根目录。
func TestCheckStaticAgainstRealSub2APIHistory(t *testing.T) {
	repo := os.Getenv("TM_SUB2API_REPO")
	if repo == "" {
		t.Skip("TM_SUB2API_REPO not set")
	}
	ctx := context.Background()
	from, err := LoadGitMigrations(ctx, repo, "v0.2.10")
	if err != nil {
		t.Skipf("cannot read v0.2.10: %v", err)
	}
	to, err := LoadGitMigrations(ctx, repo, "v0.2.14")
	if err != nil {
		t.Fatal(err)
	}
	s, err := contract.Load()
	if err != nil {
		t.Fatal(err)
	}
	r := CheckStatic(ctx, s, AppliedFromSet(from), to)
	t.Log(r.Markdown())
	want := []string{"241_add_payment_order_bonus_amount.sql", "241_add_typesafe_platform.sql"}
	if strings.Join(r.Pending, ",") != strings.Join(want, ",") {
		t.Fatalf("pending = %v, want %v", r.Pending, want)
	}
	if r.Failed(SeverityConflict) {
		t.Fatalf("0.2.10 → 0.2.14 should have no conflicts")
	}

	// 全部历史迁移都应能被扫描而不崩溃。
	for _, name := range to.Names {
		_ = AnalyzeMigration(s, name, string(to.Contents[name]))
	}
}

func keys(m map[string]Finding) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
