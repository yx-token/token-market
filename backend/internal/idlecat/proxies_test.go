package idlecat

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestSetProxyDisabledLocksBeforeGuardedWrite(t *testing.T) {
	for _, outcome := range []string{"disable", "enable", "bound", "missing", "write error", "commit error"} {
		t.Run(outcome, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			mock.ExpectBegin()
			lock := mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM proxies WHERE id = $1 FOR UPDATE")).WithArgs(int64(17))
			if outcome == "missing" {
				lock.WillReturnError(sql.ErrNoRows)
				mock.ExpectRollback()
			} else {
				lock.WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(17))
				status := "disabled"
				if outcome == "enable" {
					status = "available"
				}
				write := mock.ExpectExec(`UPDATE proxies SET status = \$2 WHERE id = \$1 AND NOT EXISTS \(SELECT 1 FROM upstreams WHERE proxy_id = proxies.id AND status <> 'removed'\)`).WithArgs(int64(17), status)
				switch outcome {
				case "bound":
					write.WillReturnResult(sqlmock.NewResult(0, 0))
					mock.ExpectRollback()
				case "write error":
					write.WillReturnError(errors.New("write failed"))
					mock.ExpectRollback()
				case "commit error":
					write.WillReturnResult(sqlmock.NewResult(0, 1))
					mock.ExpectCommit().WillReturnError(errors.New("commit failed"))
				default:
					write.WillReturnResult(sqlmock.NewResult(0, 1))
					mock.ExpectCommit()
				}
			}
			err = (&Store{Market: db}).SetProxyDisabled(context.Background(), 17, outcome != "enable")
			if (err == nil) != (outcome == "enable" || outcome == "disable") {
				t.Fatalf("unexpected result: %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestListProxiesExposesBindingsAndOriginalFields(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now().UTC().Truncate(time.Second)
	mock.ExpectQuery(`(?s)SELECT p.id,.*p.username, p.region, p.sub2api_proxy_id,.*p.created_at, p.connection_report, p.quality_report, p.connection_checked_at, p.quality_checked_at,.*assigned.ids AS assigned_upstream_ids, assigned.count AS assigned_upstream_count,.*assigned.first_id AS assigned_upstream_id.*jsonb_agg\(up.id ORDER BY up.id\).*count\(\*\)::int.*min\(up.id\).*up.proxy_id = p.id AND up.status <> 'removed'`).WillReturnRows(
		sqlmock.NewRows([]string{"id", "region", "username", "sub2api_proxy_id", "assigned_upstream_ids", "assigned_upstream_count", "assigned_upstream_id", "created_at", "connection_report"}).
			AddRow(17, "sg", "user", 91, []byte(`[3,8]`), 2, 3, now, []byte(`{"success":true}`)).
			AddRow(18, nil, nil, nil, []byte(`[]`), 0, nil, now, nil))
	rows, err := (&Store{Market: db}).ListProxies(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0]["region"] != "sg" || rows[0]["sub2api_proxy_id"] != int64(91) || rows[0]["assigned_upstream_count"] != int64(2) || rows[0]["assigned_upstream_id"] != int64(3) {
		t.Fatalf("missing proxy fields: %+v", rows)
	}
	ids, ok := rows[0]["assigned_upstream_ids"].([]any)
	if !ok || len(ids) != 2 || ids[0] != float64(3) || ids[1] != float64(8) {
		t.Fatalf("binding IDs must serialize as an array: %+v", rows[0])
	}
	empty, ok := rows[1]["assigned_upstream_ids"].([]any)
	if !ok || len(empty) != 0 || rows[1]["assigned_upstream_id"] != nil {
		t.Fatalf("unassigned proxy must expose empty IDs: %+v", rows[1])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
