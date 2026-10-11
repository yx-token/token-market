package listing

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/DueGin/token-market/internal/inspection"
)

// InspectionView 是卖家与市场共用的脱敏质检摘要，估价在读取时采用当前厂商基准。
type InspectionView struct {
	InspectionMode string                      `json:"inspection_mode"`
	QualityFactor  *float64                    `json:"quality_factor"`
	Appraisal      inspection.Appraisal        `json:"appraisal"`
	QualityChecks  map[string]inspection.Check `json:"quality_checks"`
	LastCheckedAt  *time.Time                  `json:"last_checked_at"`
}

type inspectionViewInput struct {
	EvidenceMode       string
	Health             string
	Tier               string
	Base               map[string]float64
	Factor             *float64
	AppraisedAt        *time.Time
	LastCheckedAt      *time.Time
	Checks             map[string]inspection.Check
	ConfirmedGate      bool
	CoverageIncomplete bool
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
		InspectionMode: evidenceMode,
		QualityFactor:  q,
		Appraisal:      inspection.Appraise(in.Base, in.Tier, q, in.AppraisedAt, mode != inspection.ModeFormal || evidenceMode != inspection.ModeFormal, in.CoverageIncomplete),
		QualityChecks:  inspection.PublicChecks(in.Checks),
		LastCheckedAt:  in.LastCheckedAt,
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
	rows, err := db.Query(ctx, `SELECT u.id, u.health, COALESCE(u.tier, ''), v.appraisal_base, u.appraised_at,
		q.quality_factor, COALESCE(q.checks, '{}'), q.created_at,
		EXISTS (SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(q.gates) = 'array' THEN q.gates ELSE '[]'::jsonb END) gate WHERE gate->>'confirmed' = 'true'),
		COALESCE((r.summary->'appraisal'->>'provisional')::boolean, false),
		COALESCE(r.summary->>'inspection_mode', 'trial')
		FROM upstreams u JOIN vendor_configs v ON v.platform = u.platform
		LEFT JOIN LATERAL (SELECT quality_factor, checks, gates, created_at, run_id FROM quality_verdicts
			WHERE upstream_id = u.id ORDER BY created_at DESC, id DESC LIMIT 1) q ON true
		LEFT JOIN probe_runs r ON r.id = q.run_id
		WHERE u.id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var in inspectionViewInput
		var base, checks []byte
		if err := rows.Scan(&id, &in.Health, &in.Tier, &base, &in.AppraisedAt, &in.Factor, &checks, &in.LastCheckedAt, &in.ConfirmedGate, &in.CoverageIncomplete, &in.EvidenceMode); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(base, &in.Base); err != nil {
			return nil, fmt.Errorf("读取质检估价基准：%w", err)
		}
		if err := json.Unmarshal(checks, &in.Checks); err != nil {
			return nil, fmt.Errorf("读取质检项摘要：%w", err)
		}
		out[id] = makeInspectionView(st.Mode, in)
	}
	return out, rows.Err()
}
