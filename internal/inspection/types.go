// Package inspection 定义质检目录、设置及与执行引擎无关的定价规则。
package inspection

import "time"

const (
	ModeTrial  = "trial"
	ModeFormal = "formal"
)

type TokenBudget struct {
	Input  int64 `json:"input_tokens_per_side"`
	Output int64 `json:"output_tokens_per_side"`
}

type CheckOverride struct {
	Enabled         *bool              `json:"enabled,omitempty"`
	Calibrated      bool               `json:"calibrated"`
	CalibrationNote string             `json:"calibration_note"`
	Coefficients    map[string]float64 `json:"coefficients,omitempty"`
	Thresholds      map[string]float64 `json:"thresholds,omitempty"`
}

type Settings struct {
	Mode                   string                   `json:"mode"`
	CheckOverrides         map[string]CheckOverride `json:"check_overrides"`
	GradeThresholds        map[string]float64       `json:"grade_thresholds"`
	GradeHysteresis        float64                  `json:"grade_hysteresis"`
	DowngradeConfirmations int                      `json:"downgrade_confirmations"`
	SRequirements          struct {
		GateFreeDays     int     `json:"gate_free_days"`
		AvailabilityDays int     `json:"availability_days"`
		MinAvailability  float64 `json:"min_availability"`
	} `json:"s_requirements"`
	Scheduling struct {
		Enabled      bool   `json:"enabled"`
		MinimumGrade string `json:"minimum_grade"`
		Strategy     string `json:"strategy"`
	} `json:"scheduling"`
	Timeouts struct {
		Full  int `json:"full_minutes"`
		Quick int `json:"quick_minutes"`
	} `json:"timeouts"`
	Limits struct {
		Account int `json:"account_parallelism"`
		Global  int `json:"global_parallelism"`
	} `json:"limits"`
	Recovery struct {
		AvailabilitySamples int `json:"availability_samples"`
		IntervalSeconds     int `json:"request_interval_seconds"`
	} `json:"recovery"`
	TokenBudgets map[string]TokenBudget `json:"token_budgets"`
}

type Metric struct {
	Value      *float64    `json:"value"`
	Unit       string      `json:"unit"`
	CI         *[2]float64 `json:"ci"`
	Confidence *float64    `json:"confidence"`
}

type Samples struct {
	Observed int `json:"observed"`
	Paired   int `json:"paired"`
	Excluded int `json:"excluded"`
}

type Evidence struct {
	Source           string     `json:"source"`
	WindowStart      *time.Time `json:"window_start"`
	WindowEnd        *time.Time `json:"window_end"`
	GeneratorVersion *string    `json:"generator_version"`
	SampleUnit       string     `json:"sample_unit"`
	Code             string     `json:"code"`
}

type Check struct {
	Key         string            `json:"key"`
	Name        string            `json:"name"`
	Category    string            `json:"category"`
	Status      string            `json:"status"`
	Role        string            `json:"role"`
	FactorGroup *string           `json:"factor_group"`
	Factor      *float64          `json:"factor"`
	Applied     bool              `json:"applied"`
	Metrics     map[string]Metric `json:"metrics"`
	Samples     Samples           `json:"samples"`
	Evidence    Evidence          `json:"evidence"`
	Reason      string            `json:"reason"`
	Advice      string            `json:"advice"`
	Impact      string            `json:"impact"`
	CheckedAt   time.Time         `json:"checked_at"`
}

type Gate struct {
	CheckKey   string    `json:"check_key"`
	Code       string    `json:"code"`
	Health     string    `json:"health"`
	Confirmed  bool      `json:"confirmed"`
	OccurredAt time.Time `json:"occurred_at"`
	Reason     string    `json:"reason"`
}

type Appraisal struct {
	Base        *float64   `json:"base"`
	Factor      *float64   `json:"factor"`
	Multiplier  *float64   `json:"multiplier"`
	Tier        string     `json:"tier"`
	CheckedAt   *time.Time `json:"checked_at"`
	Configured  bool       `json:"configured"`
	Provisional bool       `json:"provisional"`
}

type Aggregation struct {
	Checks        map[string]Check   `json:"checks"`
	Gates         []Gate             `json:"gates"`
	QualityFactor *float64           `json:"quality_factor"`
	GroupFactors  map[string]float64 `json:"group_factors"`
	Provisional   bool               `json:"provisional"`
}

type GradeHistory struct {
	FirstListing     bool
	GateFreeDays     int
	AvailabilityDays int
	Availability     float64
}

type GradeDecision struct {
	Grade             string
	PendingGrade      string
	PendingGradeCount int
}
