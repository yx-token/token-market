package compat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yx-token/idlicat/market/contract"
)

func TestCheckContractVersion(t *testing.T) {
	target := &MigrationSet{Desc: "v9", Names: []string{"001.sql", "002.sql"}, Contents: map[string][]byte{"001.sql": []byte("a"), "002.sql": []byte("b")}}
	current := AppliedFromSet(&MigrationSet{Names: []string{"001.sql"}, Contents: map[string][]byte{"001.sql": []byte("a")}})
	fp := contract.MigrationFingerprint([]string{"001.sql", "002.sql"})

	spec := &contract.Spec{}
	if f := CheckContractVersion(spec, "9.0.0", current, target); len(f) != 1 || f[0].Rule != "contract_version_missing" || !strings.Contains(f[0].Detail, fp) {
		t.Fatalf("missing version should block with the fingerprint to add: %+v", f)
	}
	spec.Sub2API.VerifiedVersions = []contract.VerifiedVersion{{Version: "9.0.0", MigrationFingerprints: []string{"sha256:other"}}}
	if f := CheckContractVersion(spec, "9.0.0", current, target); len(f) != 1 || f[0].Rule != "contract_fingerprint_missing" {
		t.Fatalf("wrong fingerprint should block: %+v", f)
	}
	spec.Sub2API.VerifiedVersions[0].MigrationFingerprints = append(spec.Sub2API.VerifiedVersions[0].MigrationFingerprints, fp)
	if f := CheckContractVersion(spec, "9.0.0", current, target); len(f) != 0 {
		t.Fatalf("registered version should pass: %+v", f)
	}
}

func TestProbeRoutes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/users/0/balance":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"code":401,"message":"unauthorized"}`))
		case "/api/v1/keys/0":
			// 业务 404 是 JSON，不能当成接口缺失
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"code":404,"message":"not found"}`))
		case "/v1/messages":
			w.Header().Set("X-Client-Request-ID", "abc")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(401)
		default:
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(404)
			_, _ = w.Write([]byte("404 page not found"))
		}
	}))
	defer srv.Close()
	spec := &contract.Spec{
		AdminAPI:        []contract.EndpointSpec{{Method: "POST", Path: "/api/v1/admin/users/:id/balance"}, {Method: "POST", Path: "/api/v1/admin/gone"}},
		UserAPI:         []contract.EndpointSpec{{Method: "DELETE", Path: "/api/v1/keys/:id"}},
		GatewayAPI:      []contract.EndpointSpec{{Method: "POST", Path: "/v1/messages"}},
		ResponseHeaders: map[string]string{"X-Client-Request-ID": ""},
	}
	results, findings, err := ProbeRoutes(context.Background(), srv.URL, spec, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 4 || !results[0].Present || results[1].Present || !results[2].Present || !results[3].Present {
		t.Fatalf("results: %+v", results)
	}
	if len(findings) != 1 || findings[0].Rule != "route_missing" {
		t.Fatalf("findings: %+v", findings)
	}
	if md := RoutesMarkdown(results, findings); !strings.Contains(md, "阻止升级") || !strings.Contains(md, "**缺失**") {
		t.Fatal(md)
	}

	spec.ResponseHeaders["X-Other"] = ""
	if _, findings, _ = ProbeRoutes(context.Background(), srv.URL, spec, srv.Client()); len(findings) != 2 {
		t.Fatalf("missing response header should be reported: %+v", findings)
	}
}
