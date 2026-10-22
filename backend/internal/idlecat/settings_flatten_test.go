package idlecat

import "testing"

func TestFlattenAdminSettingsUsesDefaultsWhenEmpty(t *testing.T) {
	out, err := flattenAdminSettings(map[string]any{
		"market_discovery": map[string]any{"hot_bar": map[string]any{"enabled": true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out["commission_rate"] != 0.2 {
		t.Fatalf("commission_rate=%v want 0.2", out["commission_rate"])
	}
	if out["freeze_hours"] != float64(24) {
		t.Fatalf("freeze_hours=%v want 24", out["freeze_hours"])
	}
	if _, ok := out["market_discovery"]; !ok {
		t.Fatal("market_discovery should be preserved")
	}
	if _, ok := out["platform"]; ok {
		t.Fatal("nested platform blob should not be exposed")
	}
}

func TestFlattenAdminSettingsOverlaysPlatformBlob(t *testing.T) {
	out, err := flattenAdminSettings(map[string]any{
		"platform": map[string]any{
			"commission_rate": 0.05,
			"withdraw_min":    20.0,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out["commission_rate"] != 0.05 {
		t.Fatalf("commission_rate=%v", out["commission_rate"])
	}
	if out["withdraw_min"] != 20.0 {
		t.Fatalf("withdraw_min=%v", out["withdraw_min"])
	}
	if out["quote_validity_days"] != float64(7) {
		t.Fatalf("quote_validity_days=%v", out["quote_validity_days"])
	}
}

func TestFlattenAdminSettingsOverlaysLegacyTopLevelKeys(t *testing.T) {
	out, err := flattenAdminSettings(map[string]any{
		"commission_rate": 0.33,
		"platform": map[string]any{
			"commission_rate": 0.05,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out["commission_rate"] != 0.33 {
		t.Fatalf("commission_rate=%v want legacy top-level 0.33", out["commission_rate"])
	}
}

func TestOverlayPlatformJSONRejectsBadCommission(t *testing.T) {
	_, err := overlayPlatformJSON(nil, []byte(`{"commission_rate": 9}`))
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestPlatformFieldNamesCoversAdminForm(t *testing.T) {
	fields := platformFieldNames()
	for _, k := range []string{
		"commission_rate", "freeze_hours", "withdraw_min",
		"quote_validity_days", "manual_retest_daily_limit", "refund_lock_seconds",
	} {
		if _, ok := fields[k]; !ok {
			t.Fatalf("missing platform field %s", k)
		}
	}
}
