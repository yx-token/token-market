package listing

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/yx-token/idlicat/market/internal/inspection"
)

// InspectionView 是卖家与市场共用的脱敏质检摘要，估价在读取时采用当前厂商基准。
type InspectionView struct {
	DetectionStatus string                      `json:"detection_status"`
	ModelChecks     []ModelDetection            `json:"model_checks"`
	InspectionMode  string                      `json:"inspection_mode"`
	QualityFactor   *float64                    `json:"quality_factor"`
	Appraisal       inspection.Appraisal        `json:"appraisal"`
	QualityChecks   map[string]inspection.Check `json:"quality_checks"`
	LastCheckedAt   *time.Time                  `json:"last_checked_at"`
}

type inspectionViewInput struct {
	Models             []string
	EvidenceMode       string
	Health             string
	Tier               string
	Base               map[string]float64
	Factor             *float64
	AppraisedAt        *time.Time
	LastCheckedAt      *time.Time
	DetectionChecks    map[string]inspection.Check
	Checks             map[string]inspection.Check
	ConfirmedGate      bool
	CoverageIncomplete bool
	PricingEnabled     bool
}

func makeInspectionView(mode string, in inspectionViewInput) InspectionView {
	evidenceMode := in.EvidenceMode
	if evidenceMode != inspection.ModeFormal {
		evidenceMode = inspection.ModeTrial
	}
	q := in.Factor
	if in.AppraisedAt == nil || in.ConfirmedGate || in.Health == HealthDegraded || in.Health == HealthUnavailable || (q != nil && (*q < 0.5 || *q > 1.05 || math.IsNaN(*q) || math.IsInf(*q, 0))) {
		q = nil
	}
	return InspectionView{
		DetectionStatus: detectionStatus(in.LastCheckedAt, in.DetectionChecks),
		ModelChecks:     modelDetections(in.LastCheckedAt, in.Models, in.DetectionChecks),
		InspectionMode:  evidenceMode,
		QualityFactor:   q,
		Appraisal:       inspection.Appraise(in.Base, in.Tier, q, in.AppraisedAt, !in.PricingEnabled, in.CoverageIncomplete),
		QualityChecks:   inspection.PublicChecks(in.Checks),
		LastCheckedAt:   in.LastCheckedAt,
	}
}

// LoadInspectionViews 批量读取最近质检证据，避免列表页按上游逐条查询。
// upstreams 的默认 Q=1 仅供兼容排序，不能据此向卖家显示已经完成检测。
func LoadInspectionViews(ctx context.Context, db DB, ids []int64) (map[int64]InspectionView, error) {
	out := map[int64]InspectionView{}
	if len(ids) == 0 {
		return out, nil
	}
	st, err := inspection.Load(ctx, db)
	if err != nil {
		return nil, err
	}
	pricingEnabled := st.PricingEnabled()
	rows, err := db.Query(ctx, `SELECT u.id, u.health, COALESCE(u.tier, ''), v.appraisal_base, CASE WHEN r.status = 'done' AND r.upstream_id = u.id THEN u.appraised_at END,
		CASE WHEN r.status = 'done' AND r.upstream_id = u.id THEN COALESCE(q.quality_factor, u.quality_factor, 1) END, COALESCE(q.checks, '{}'),
		CASE WHEN r.status = 'done' AND r.upstream_id = u.id AND r.finished_at > COALESCE(state.deductions_reset_at, '-infinity'::timestamptz) THEN q.created_at END,
		EXISTS (SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(q.gates) = 'array' THEN q.gates ELSE '[]'::jsonb END) gate WHERE gate->>'confirmed' = 'true'),
		COALESCE((r.summary->'appraisal'->>'provisional')::boolean, false),
		COALESCE(r.summary->>'inspection_mode', 'trial'), COALESCE(u.model_whitelist, '{}'), jsonb_build_object('inspection_settings',r.execution_snapshot->'inspection_settings','policy',jsonb_build_object('flows',r.execution_snapshot->'policy'->'flows')), state.deductions_reset_at
		FROM upstreams u JOIN vendor_configs v ON v.platform = u.platform
		LEFT JOIN upstream_quality_state state ON state.upstream_id = u.id
		LEFT JOIN LATERAL (SELECT quality_factor, checks, gates, created_at, run_id FROM quality_verdicts
			WHERE upstream_id = u.id AND created_at > COALESCE(state.deductions_reset_at, '-infinity'::timestamptz)
			ORDER BY created_at DESC, id DESC LIMIT 1) q ON true
		LEFT JOIN probe_runs r ON r.id = q.run_id
		WHERE u.id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var in inspectionViewInput
		in.PricingEnabled = pricingEnabled
		var base, checks, snapshot []byte
		var reset *time.Time
		if err := rows.Scan(&id, &in.Health, &in.Tier, &base, &in.AppraisedAt, &in.Factor, &checks, &in.LastCheckedAt, &in.ConfirmedGate, &in.CoverageIncomplete, &in.EvidenceMode, &in.Models, &snapshot, &reset); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(base, &in.Base); err != nil {
			return nil, fmt.Errorf("读取质检估价基准：%w", err)
		}
		if err := json.Unmarshal(checks, &in.Checks); err != nil {
			return nil, fmt.Errorf("读取质检项摘要：%w", err)
		}
		in.DetectionChecks = currentDetectionChecks(in.Checks, snapshot, st, reset, time.Now())
		out[id] = makeInspectionView(st.Mode, in)
	}
	return out, rows.Err()
}
