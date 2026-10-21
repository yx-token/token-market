package contract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmbeddedContractLoads(t *testing.T) {
	s, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !s.HasColumn("usage_logs", "request_id") || !s.HasColumn("USAGE_LOGS", "Account_Rate_Multiplier") {
		t.Fatal("usage_logs columns missing from contract")
	}
	if tbl, _ := s.Table("usage_logs"); !tbl.Large {
		t.Fatal("usage_logs should be marked large")
	}
	if _, ok := s.Verified("v0.2.14"); !ok {
		t.Fatal("0.2.14 should be a verified version")
	}
}

func TestEveryQueryOnlyUsesContractTables(t *testing.T) {
	s, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	qs, err := Queries()
	if err != nil {
		t.Fatal(err)
	}
	if len(qs) == 0 {
		t.Fatal("no queries embedded")
	}
	for name, sql := range qs {
		lower := strings.ToLower(sql)
		found := false
		for table := range s.Tables {
			if strings.Contains(lower, "from "+table) || strings.Contains(lower, "join "+table) {
				found = true
			}
		}
		if !found {
			t.Errorf("query %s does not reference any contract table", name)
		}
	}
}

func TestMigrationFingerprintIsOrderIndependent(t *testing.T) {
	a := MigrationFingerprint([]string{"002_b.sql", "001_a.sql"})
	b := MigrationFingerprint([]string{"001_a.sql", "002_b.sql"})
	if a != b || !strings.HasPrefix(a, "sha256:") {
		t.Fatalf("fingerprints differ: %s %s", a, b)
	}
	if a == MigrationFingerprint([]string{"001_a.sql"}) {
		t.Fatal("different sets must have different fingerprints")
	}
}

func TestParseRejectsUnknownVersion(t *testing.T) {
	if _, err := Parse([]byte("version: 2\n")); err == nil {
		t.Fatal("expected error")
	}
}

// 宿主迁移指纹必须与同仓 backend/migrations 一致，否则换上新宿主后市场会进入安全模式。
func TestHostMigrationFingerprintMatchesBackend(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "backend", "migrations", "*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		if _, statErr := os.Stat(filepath.Join("..", "..", "backend")); os.IsNotExist(statErr) {
			t.Skip("backend/ not present (market-only checkout)")
		}
		t.Fatal("backend/migrations has no .sql files")
	}
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = filepath.Base(f)
	}
	s, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if fp := MigrationFingerprint(names); !s.HostMigrations(fp) {
		t.Fatalf("contract.yaml host_migration_fingerprints must include %s (%d backend migrations)", fp, len(names))
	}
}
