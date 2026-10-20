package main

import (
	"context"
	"encoding/json"
	"errors"
	"expvar"
	"log/slog"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// 运行指标通过 expvar 暴露在管理端口的 /debug/vars，可由 Prometheus 的 json exporter 或巡检脚本采集。
var (
	jobRuns     = expvar.NewMap("job_runs")
	jobFailures = expvar.NewMap("job_failures")
	jobLastOK   = expvar.NewMap("job_last_success_unix")
	isLeader    atomic.Bool
)

func recordJob(name string, err error) {
	jobRuns.Add(name, 1)
	if err != nil {
		jobFailures.Add(name, 1)
		return
	}
	v := new(expvar.Int)
	v.Set(time.Now().Unix())
	jobLastOK.Set(name, v)
}

// marketGauges 在采集时查询数据库，返回需要告警的积压与异常数量。
func marketGauges(pool *pgxpool.Pool) func() any {
	return func() any {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		out := map[string]any{}
		count := func(name, sql string) {
			var n int64
			if err := pool.QueryRow(ctx, sql).Scan(&n); err != nil {
				out[name+"_error"] = err.Error()
				return
			}
			out[name] = n
		}
		count("ops_pending", "SELECT count(*) FROM sub2api_ops WHERE status IN ('pending', 'sending', 'failed')")
		count("ops_dead", "SELECT count(*) FROM sub2api_ops WHERE status = 'dead'")
		count("ops_oldest_pending_seconds", "SELECT COALESCE(EXTRACT(EPOCH FROM now() - min(created_at))::bigint, 0) FROM sub2api_ops WHERE status IN ('pending', 'sending', 'failed')")
		count("anomalies_open", "SELECT count(*) FROM billing_anomalies WHERE status = 'open'")
		count("recharges_crediting", "SELECT count(*) FROM recharge_orders WHERE status = 'crediting'")
		count("recharges_failed", "SELECT count(*) FROM recharge_orders WHERE status = 'failed'")
		count("refunds_active", "SELECT count(*) FROM refunds WHERE status IN ('locking', 'subtracting', 'paying')")
		count("withdrawals_pending_review", "SELECT count(*) FROM withdrawals WHERE status = 'pending_review'")
		count("keys_provisioning", "SELECT count(*) FROM buyer_keys WHERE status = 'provisioning'")
		count("probe_runs_queued", "SELECT count(*) FROM probe_runs WHERE status = 'queued'")
		count("upstreams_degraded", "SELECT count(*) FROM upstreams WHERE health IN ('degraded', 'unavailable') AND status IN ('ready', 'listed')")
		count("usage_ingest_lag_seconds", "SELECT COALESCE(EXTRACT(EPOCH FROM now() - max(created_at))::bigint, -1) FROM usage_records")
		var state string
		if err := pool.QueryRow(ctx, "SELECT guard_state FROM compat_state WHERE id = 1").Scan(&state); err == nil {
			out["compat_guard_state"] = state
		}
		out["leader"] = isLeader.Load()
		return out
	}
}

func serveAdmin(ctx context.Context, addr string, pool *pgxpool.Pool, log *slog.Logger) {
	expvar.Publish("market", expvar.Func(marketGauges(pool)))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		status := http.StatusOK
		if err := pool.Ping(r.Context()); err != nil {
			status = http.StatusServiceUnavailable
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"leader": strconv.FormatBool(isLeader.Load())})
	})
	mux.Handle("GET /debug/vars", expvar.Handler())
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("worker admin server", "err", err)
	}
}
