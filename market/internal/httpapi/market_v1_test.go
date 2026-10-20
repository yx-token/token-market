package httpapi

import "testing"

func TestMarketV1Pattern(t *testing.T) {
	cases := map[string]string{
		"GET /api/admin/vendors":                       "", // 原生 idlecat
		"GET /api/admin/sub2api/status":                "", // 原生 idlecat
		"GET /api/admin/supply/accounts":               "", // 原生 idlecat
		"PUT /api/admin/vendors/{platform}":            "", // 原生 idlecat
		"POST /api/admin/proxies/{id}/disable":         "",
		"POST /api/admin/probe-items/{id}/retire":      "",
		"POST /api/admin/recharges/{id}/confirm":       "",
		"POST /api/admin/recharges/{id}/refund":        "",
		"POST /api/admin/refunds/{id}/paid":            "",
		"POST /api/admin/withdrawals/{id}/review":      "",
		"POST /api/admin/withdrawals/{id}/paid":        "",
		"POST /api/admin/appeals/{id}/resolve":         "",
		"GET /api/admin/inspection/checks":             "",
		"PUT /api/admin/inspection/checks/{key}":       "",
		"PUT /api/admin/payments/merchants/{provider}": "",
		"GET /api/admin/identities":                    "", // 原生 idlecat
		"POST /api/admin/identities/ensure":            "", // 原生 idlecat
		"POST /api/admin/identities/bind":              "",
		"GET /api/admin/system/version":                "GET /api/v1/admin/market/system/version",
		"GET /api/admin/ledger":                        "",
		"GET /api/admin/org/roles":                     "",
		"POST /api/admin/engine/session":               "",
		"GET /api/buyer/wallet":                        "",
	}
	for in, want := range cases {
		if got := marketV1Pattern(in); got != want {
			t.Fatalf("%q => %q, want %q", in, got, want)
		}
	}
}
