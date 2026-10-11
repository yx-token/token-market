package quality

import (
	"context"

	"github.com/DueGin/token-market/internal/inspection"
)

// 只累计当前及上一轮恢复的门槛后新样本，不用暂停前生产记录或无限历史证明修复。
func (e *Engine) inspectRecoveryAvailability(ctx context.Context, run runRow, u *upstreamRow, st InspectionSettings, checks map[string]inspection.Check, gates *[]inspection.Gate) error {
	if u.health != "degraded" && u.health != "unavailable" {
		return nil
	}
	rows, err := e.DB.Query(ctx, `WITH recent AS (
 SELECT r.id FROM probe_runs r WHERE r.upstream_id=$1 AND r.model=$2 AND r.id<=$3
 AND (r.id=$3 OR r.status IN ('done','failed')) AND r.mode='full'
 AND r.execution_snapshot->>'inspection_mode'='formal'
 AND r.execution_snapshot->'inspection_settings'=$4::jsonb
 AND EXISTS(SELECT 1 FROM probe_results pr WHERE pr.run_id=r.id AND pr.generator_version='availability_recovery/2')
 ORDER BY r.id DESC LIMIT 2
 ), gate_time AS (
 SELECT max(v.created_at) AS at FROM quality_verdicts v WHERE v.upstream_id=$1
 AND EXISTS(SELECT 1 FROM jsonb_array_elements(v.gates) g WHERE g->>'confirmed'='true')
 ) SELECT pr.run_id,COALESCE(pr.http_status,0),COALESCE(pr.error_class,''),pr.flags
 FROM probe_results pr JOIN recent ON recent.id=pr.run_id CROSS JOIN gate_time
 WHERE pr.generator_version='availability_recovery/2' AND pr.created_at>gate_time.at
 AND pr.created_at>now()-interval '7 days' AND pr.step_state->>'complete'='true'`, u.id, run.model, run.id, mustJSON(st))
	if err != nil {
		return err
	}
	defer rows.Close()
	var observed, total, success, window, limited, currentTotal, currentWindow int
	for rows.Next() {
		var runID int64
		var status int
		var class string
		var flags []string
		if err := rows.Scan(&runID, &status, &class, &flags); err != nil {
			return err
		}
		observed++
		if class != "rate_limited" && class != "platform" && class != "configuration" && class != "expected_reject" {
			total++
			if runID == run.id {
				currentTotal++
			}
			if status >= 200 && status < 300 && class == "" {
				success++
			}
		}
		if containsAny(flags, "window_available") && (class == "" || containsAny(flags, "abnormal_rate_limited")) {
			window++
			if runID == run.id {
				currentWindow++
			}
			if containsAny(flags, "abnormal_rate_limited") {
				limited++
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if observed == 0 {
		return nil
	}
	for _, key := range []string{"D13", "D04"} {
		d, _ := inspection.Effective(key, st)
		if !d.Enabled {
			continue
		}
		c := newInspectionCheck(d, e.now())
		c.Status = "inconclusive"
		c.Reason = "最近两轮门槛后恢复样本不足，等待下一次复测。"
		c.Evidence = inspection.Evidence{Source: "recovery_probe", SampleUnit: "request", Code: "recovery_samples_insufficient"}
		if key == "D13" {
			c.Samples = inspection.Samples{Observed: observed, Paired: total, Excluded: observed - total}
			if total >= int(d.Thresholds["recovery_min_samples"]) && total > 0 && currentTotal > 0 {
				rate := float64(success) / float64(total)
				applyAvailabilityRate(&c, rate, d)
				if rate < d.Thresholds["gate_rate"] {
					*gates = append(*gates, inspection.Gate{CheckKey: key, Code: "recovery_availability_low", Health: "unavailable", Confirmed: d.Calibrated, OccurredAt: e.now(), Reason: "门槛后恢复样本仍显示可用率低于最低要求。"})
				}
			}
		} else {
			c.Samples = inspection.Samples{Observed: observed, Paired: window, Excluded: observed - window}
			if window >= int(d.Thresholds["recovery_min_samples"]) && window > 0 && currentWindow > 0 {
				rate := float64(limited) / float64(window)
				c.Status, c.Factor = "pass", numberPointer(1)
				c.Reason = "最近两轮门槛后新样本未见频繁异常限流。"
				c.Metrics["abnormal_429_rate"] = metric(rate, "fraction")
				if rate > d.Thresholds["severe_rate"] {
					c.Status, c.Factor = "fail", numberPointer(d.Coefficients["severe"])
				} else if rate >= d.Thresholds["warning_rate"] {
					c.Status, c.Factor = "warning", numberPointer(d.Coefficients["warning"])
				}
			}
		}
		checks[key] = c
	}
	return nil
}
