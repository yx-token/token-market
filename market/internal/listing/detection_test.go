package listing

import (
	"testing"
	"time"

	"github.com/yx-token/token-market/market/internal/inspection"
)

func TestDetectionRequiresObservedModelEvidence(t *testing.T) {
	at := time.Now()
	models := []string{"gpt-6-astra", "gpt-6-sol", "new-model"}
	checks := map[string]inspection.Check{"D14": {Status: "pass", Samples: inspection.Samples{Observed: 20}, Metrics: map[string]inspection.Metric{"models": {Models: []inspection.ModelCheckResult{
		{Model: "gpt-6-astra", Status: "pass", Samples: 20}, {Model: "gpt-6-sol", Status: "fail", Samples: 20},
	}}}}}
	got := modelDetections(&at, models, checks)
	if got[0].DegradationStatus != "passed" || got[0].DetectionStatus != "tested" {
		t.Fatalf("astra needs its own observed result: %+v", got)
	}
	if got[1].DegradationStatus != "" || got[2].DetectionStatus != "untested" {
		t.Fatalf("other models must not inherit astra's result: %+v", got)
	}
	if detectionStatus(nil, checks) != "untested" || detectionStatus(&at, checks) != "tested" {
		t.Fatal("a stored default grade or a missing run must not count as detection")
	}
	if got := modelDetections(nil, models, checks); got[0].DetectionStatus != "untested" || got[0].DegradationStatus != "untested" {
		t.Fatalf("missing completed-run evidence must invalidate model result: %+v", got)
	}
	checks["D14"] = inspection.Check{Status: "pass", Metrics: map[string]inspection.Metric{"models": {Models: []inspection.ModelCheckResult{{Model: "gpt-6-astra", Status: "pass", Samples: 0}}}}}
	if got := modelDetections(&at, models, checks); got[0].DegradationStatus != "untested" {
		t.Fatalf("zero samples must never appear as passed: %+v", got)
	}
}

func TestAstraIntelligenceIsNotDegradationEvidence(t *testing.T) {
	checks := map[string]inspection.Check{"D07": {Metrics: map[string]inspection.Metric{"models": {Models: []inspection.ModelCheckResult{{Model: "gpt-6-astra", Status: "pass", Samples: 30}}}}}}
	at := time.Now()
	got := modelDetections(&at, []string{"gpt-6-astra"}, checks)[0]
	if got.DetectionStatus != "tested" || got.DegradationStatus != "untested" {
		t.Fatalf("intelligence cannot stand in for degradation evidence: %+v", got)
	}
}
