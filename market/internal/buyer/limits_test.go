package buyer

import (
	"encoding/json"
	"testing"
)

// 花费限额按人民币计，键名为 *_cny；没刷新的旧前端仍提交 *_usd，必须照样生效，不能在保存时把限额清空。
func TestLimitsJSONUsesCNYKeysAndAcceptsLegacyUSDKeys(t *testing.T) {
	var legacy Limits
	if err := json.Unmarshal([]byte(`{"quota_usd":5,"rate_limit_5h_usd":1,"rate_limit_1d_usd":2,"rate_limit_7d_usd":3,"ip_whitelist":["10.0.0.1"]}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.QuotaCNY == nil || *legacy.QuotaCNY != 5 || legacy.RateLimit5h == nil || *legacy.RateLimit5h != 1 ||
		legacy.RateLimit1d == nil || *legacy.RateLimit1d != 2 || legacy.RateLimit7d == nil || *legacy.RateLimit7d != 3 ||
		len(legacy.IPWhitelist) != 1 {
		t.Fatalf("legacy keys not accepted: %+v", legacy)
	}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"quota_cny":5,"rate_limit_5h_cny":1,"rate_limit_1d_cny":2,"rate_limit_7d_cny":3,"ip_whitelist":["10.0.0.1"]}` {
		t.Fatalf("limits must be stored and returned with CNY keys only: %s", raw)
	}

	var both Limits
	if err := json.Unmarshal([]byte(`{"quota_cny":8,"quota_usd":5}`), &both); err != nil {
		t.Fatal(err)
	}
	if both.QuotaCNY == nil || *both.QuotaCNY != 8 || both.RateLimit5h != nil {
		t.Fatalf("new key must win over legacy key: %+v", both)
	}

	var empty Limits
	if err := json.Unmarshal([]byte(`{}`), &empty); err != nil {
		t.Fatal(err)
	}
	if empty.QuotaCNY != nil || empty.RateLimit5h != nil || empty.RateLimit1d != nil || empty.RateLimit7d != nil {
		t.Fatalf("absent limits must stay unset: %+v", empty)
	}
}
