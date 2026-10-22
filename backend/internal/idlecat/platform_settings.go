package idlecat

import (
	"encoding/json"
	"fmt"
	"strings"
)

// 与 market/internal/settings.Defaults 对齐（backend 不能 import market/internal）。
// 管理端设置页依赖这些扁平字段；无 platform 行时也要能读到默认值。
var platformDefaultsJSON = []byte(`{
  "commission_rate": 0.2,
  "freeze_hours": 24,
  "freeze_days": 1,
  "credit_usd_to_balance_rate": 1,
  "withdraw_min": 10,
  "withdraw_auto_approve_max": 100,
  "manual_retest_daily_limit": 3,
  "quote_validity_days": 7,
  "grade_coefficients": {"A": 1, "B": 0.85, "C": 0.7, "D": 0, "S": 1.2},
  "price_change_cooldown_minutes": 60,
  "shadow_concurrency": 5,
  "refund_lock_seconds": 60,
  "new_seller_days": 30,
  "new_seller_direct_cap": 500,
  "scheduler_change_budget": 50,
  "balance_mismatch_tolerance": 0.05,
  "allow_no_proxy": false,
  "require_seller_proxy": true,
  "price_models": ["claude-opus-4-1", "claude-sonnet-4-5", "claude-haiku-4-5", "gpt-5.1", "gpt-5.1-codex", "gemini-2.5-pro", "gemini-2.5-flash", "grok-4", "kimi-for-coding", "glm-4.6", "MiniMax-M2"],
  "payment_public_url": "",
  "payment_channels": {"alipay_f2f": true, "alipay_page": true, "manual": true, "wechat_native": true},
  "recharge_min": 1,
  "recharge_max": 50000,
  "recharge_order_timeout_minutes": 30,
  "refund_original_route": true,
  "refund_original_route_days": 90,
  "probe_request_timeout_seconds": 180,
  "probe_batch_timeout_minutes": 30
}`)

func platformFieldNames() map[string]struct{} {
	var m map[string]any
	_ = json.Unmarshal(platformDefaultsJSON, &m)
	out := make(map[string]struct{}, len(m))
	for k := range m {
		out[k] = struct{}{}
	}
	return out
}

func platformDefaultsMap() map[string]any {
	var m map[string]any
	_ = json.Unmarshal(platformDefaultsJSON, &m)
	return m
}

// overlayPlatformJSON: Defaults ← stored ← patch，并同步 freeze_hours/days。
func overlayPlatformJSON(stored, patch []byte) ([]byte, error) {
	out := platformDefaultsMap()
	if err := mergeJSONObject(out, stored); err != nil {
		return nil, fmt.Errorf("decode settings: %w", err)
	}
	var patchFields map[string]json.RawMessage
	if len(patch) > 0 && string(patch) != "null" {
		if err := json.Unmarshal(patch, &patchFields); err != nil {
			return nil, fmt.Errorf("decode patch: %w", err)
		}
		if err := mergeJSONObject(out, patch); err != nil {
			return nil, fmt.Errorf("decode patch: %w", err)
		}
	}
	syncFreezeFields(out, patchFields)
	if url, ok := out["payment_public_url"].(string); ok {
		out["payment_public_url"] = strings.TrimRight(strings.TrimSpace(url), "/")
	}
	if err := validatePlatformMap(out); err != nil {
		return nil, err
	}
	return json.Marshal(out)
}

func mergeJSONObject(dst map[string]any, raw []byte) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var src map[string]any
	if err := json.Unmarshal(raw, &src); err != nil {
		return err
	}
	if src == nil {
		return fmt.Errorf("settings must be an object")
	}
	for k, v := range src {
		dst[k] = v
	}
	return nil
}

func syncFreezeFields(out map[string]any, patchFields map[string]json.RawMessage) {
	hours, hasHours := asInt(out["freeze_hours"])
	days, hasDays := asInt(out["freeze_days"])
	if patchFields != nil {
		if _, ok := patchFields["freeze_hours"]; ok {
			out["freeze_days"] = (hours + 23) / 24
			return
		}
		if _, ok := patchFields["freeze_days"]; ok && hasDays {
			out["freeze_hours"] = days * 24
			out["freeze_days"] = days
			return
		}
	}
	if hasHours {
		out["freeze_days"] = (hours + 23) / 24
	}
}

func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	default:
		return 0, false
	}
}

func asFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func validatePlatformMap(m map[string]any) error {
	rate, _ := asFloat(m["commission_rate"])
	if rate < 0 || rate > 5 {
		return fmt.Errorf("commission_rate must be in [0, 5]")
	}
	fh, _ := asInt(m["freeze_hours"])
	if fh < 0 {
		return fmt.Errorf("freeze_hours must be >= 0")
	}
	wm, _ := asFloat(m["withdraw_min"])
	if wm < 0 {
		return fmt.Errorf("withdraw limits must be >= 0")
	}
	qvd, _ := asInt(m["quote_validity_days"])
	if qvd <= 0 {
		return fmt.Errorf("quote_validity_days must be > 0")
	}
	mrd, _ := asInt(m["manual_retest_daily_limit"])
	if mrd < 0 {
		return fmt.Errorf("manual_retest_daily_limit must be >= 0")
	}
	rls, _ := asInt(m["refund_lock_seconds"])
	if rls < 0 {
		return fmt.Errorf("refund_lock_seconds must be >= 0")
	}
	return nil
}

// flattenAdminSettings 对齐 market GET /api/admin/settings：扁平 platform 字段 + 其它 KV。
func flattenAdminSettings(keys map[string]any) (map[string]any, error) {
	platFields := platformFieldNames()
	var stored []byte
	if plat, ok := keys["platform"]; ok && plat != nil {
		b, err := json.Marshal(plat)
		if err != nil {
			return nil, err
		}
		stored = b
	}
	topPatch := map[string]any{}
	for k, v := range keys {
		if _, ok := platFields[k]; ok {
			topPatch[k] = v
		}
	}
	var patchRaw []byte
	if len(topPatch) > 0 {
		b, err := json.Marshal(topPatch)
		if err != nil {
			return nil, err
		}
		patchRaw = b
	}
	flatRaw, err := overlayPlatformJSON(stored, patchRaw)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if err := json.Unmarshal(flatRaw, &out); err != nil {
		return nil, err
	}
	for k, v := range keys {
		if k == "platform" {
			continue
		}
		if _, isPlat := platFields[k]; isPlat {
			continue
		}
		out[k] = v
	}
	return out, nil
}
