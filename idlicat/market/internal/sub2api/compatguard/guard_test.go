package compatguard

import (
	"context"
	"errors"
	"testing"

	"github.com/yx-token/idlicat/market/contract"
)

type fakeVersion struct {
	v   string
	err error
}

func (f fakeVersion) GetVersion(context.Context) (string, error) { return f.v, f.err }

type fakeMigrations struct {
	names []string
	err   error
}

func (f fakeMigrations) AppliedMigrationNames(context.Context) ([]string, error) {
	return f.names, f.err
}

func specWith(t *testing.T, fingerprint string) *contract.Spec {
	t.Helper()
	s, err := contract.Parse([]byte(`
version: 1
sub2api:
  verified_versions:
    - version: "0.2.14"
      migration_fingerprints: ["` + fingerprint + `"]
`))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestEvaluate(t *testing.T) {
	names := []string{"001_init.sql", "002_x.sql"}
	spec := specWith(t, contract.MigrationFingerprint(names))

	if r := Evaluate(spec, "v0.2.14", names); r.State != StateNormal {
		t.Fatalf("verified version should be normal: %+v", r)
	}
	if r := Evaluate(spec, "0.2.15", names); r.State != StateSafeMode || r.Reason == "" {
		t.Fatalf("unknown version should be safe mode: %+v", r)
	}
	if r := Evaluate(spec, "0.2.14", append(names, "003_y.sql")); r.State != StateSafeMode {
		t.Fatalf("unexpected migration set should be safe mode: %+v", r)
	}
}

func TestGuardStartsInSafeModeAndKeepsStateOnReadError(t *testing.T) {
	names := []string{"001_init.sql"}
	spec := specWith(t, contract.MigrationFingerprint(names))
	ctx := context.Background()

	g := New(spec, fakeVersion{v: "0.2.14"}, fakeMigrations{names: names}, nil, nil)
	if ok, _ := g.AllowWrites(ctx); ok {
		t.Fatal("writes must be blocked before the first check")
	}
	if _, err := g.Check(ctx); err != nil {
		t.Fatal(err)
	}
	if ok, _ := g.AllowWrites(ctx); !ok {
		t.Fatal("writes should be allowed after a successful check")
	}

	g.versions = fakeVersion{err: errors.New("sub2api down")}
	if _, err := g.Check(ctx); err == nil {
		t.Fatal("expected error")
	}
	if g.State() != StateNormal {
		t.Fatal("read errors must not flip the state")
	}
}
