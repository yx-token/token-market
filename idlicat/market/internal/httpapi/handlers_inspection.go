package httpapi

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/yx-token/idlicat/market/internal/inspection"
	"github.com/yx-token/idlicat/market/internal/platform/httpx"
)

func inspectionSettingsView(st inspection.Settings) any {
	return struct {
		inspection.Settings
		Defaults []inspection.ModelCapability `json:"model_capability_defaults"`
	}{st, inspection.CapabilityDefaults()}
}

func (s *Server) inspectionSettings(w http.ResponseWriter, r *http.Request) error {
	st, err := inspection.Load(r.Context(), s.DB)
	if err != nil {
		return err
	}
	return ok(w, inspectionSettingsView(st))
}

func (s *Server) updateInspectionSettings(w http.ResponseWriter, r *http.Request) error {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return err
	}
	candidate, err := inspection.Load(r.Context(), s.DB)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, &candidate); err != nil {
		return httpx.BadRequest("INVALID_INSPECTION_SETTINGS", err.Error())
	}
	platform, err := s.settings(r.Context())
	if err != nil {
		return err
	}
	if candidate.Timeouts.Full*60 < platform.ProbeRequestTimeoutSeconds || candidate.Timeouts.Quick*60 < platform.ProbeRequestTimeoutSeconds {
		return httpx.BadRequest("INVALID_INSPECTION_SETTINGS", "质检截止时间不能短于单次请求时限")
	}
	st, err := inspection.Update(r.Context(), s.DB, raw)
	if err != nil {
		return httpx.BadRequest("INVALID_INSPECTION_SETTINGS", err.Error())
	}
	s.audit(r, "inspection.settings.update", "inspection", json.RawMessage(raw))
	if s.Kicker != nil {
		s.Kicker.Kick(r.Context())
	}
	return ok(w, inspectionSettingsView(st))
}

func (s *Server) inspectionChecks(w http.ResponseWriter, r *http.Request) error {
	st, err := inspection.Load(r.Context(), s.DB)
	if err != nil {
		return err
	}
	result := make([]inspection.EffectiveDefinition, 0)
	for _, definition := range inspection.Known() {
		effective, _ := inspection.Effective(definition.Key, st)
		result = append(result, effective)
	}
	return ok(w, result)
}

func (s *Server) updateInspectionCheck(w http.ResponseWriter, r *http.Request) error {
	var in inspection.CheckOverride
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	st, err := inspection.UpdateOverride(r.Context(), s.DB, r.PathValue("key"), in)
	if err != nil {
		return httpx.BadRequest("INVALID_INSPECTION_CHECK", err.Error())
	}
	s.audit(r, "inspection.check.update", r.PathValue("key"), in)
	result, _ := inspection.Effective(r.PathValue("key"), st)
	return ok(w, result)
}
