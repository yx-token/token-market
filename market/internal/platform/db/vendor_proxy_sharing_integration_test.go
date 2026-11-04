//go:build integration

package db_test

import (
	"context"
	"fmt"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/yx-token/token-market/market/internal/platform/db"
	"github.com/yx-token/token-market/market/internal/testutil"
	"github.com/yx-token/token-market/market/migrations"
)

func TestVendorProxyPolicyMigration(t *testing.T) {
	for _, value := range []string{"", "{}", `{"require_seller_proxy":true}`, `{"require_seller_proxy":false}`} {
		t.Run(fmt.Sprintf("legacy_%s", value), func(t *testing.T) {
			_, pool := testutil.NewDatabase(t)
			ctx := context.Background()
			legacy := fstest.MapFS{}
			names, err := fs.Glob(migrations.FS, "*.sql")
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range names {
				if name >= "0048" {
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
			if value != "" {
				if _, err := pool.Exec(ctx, `INSERT INTO settings(key,value) VALUES('platform',$1)`, value); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := db.Migrate(ctx, pool, migrations.FS); err != nil {
				t.Fatal(err)
			}
			want := value != `{"require_seller_proxy":false}`
			var count, wrong int
			if err := pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE require_seller_proxy <> $1) FROM vendor_configs`, want).Scan(&count, &wrong); err != nil || count == 0 || wrong != 0 {
				t.Fatalf("vendor initialization: count=%d wrong=%d err=%v", count, wrong, err)
			}
			var remains bool
			if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM settings WHERE key='platform' AND value ? 'require_seller_proxy')`).Scan(&remains); err != nil || remains {
				t.Fatalf("global setting remains: %v %v", remains, err)
			}
		})
	}
}
