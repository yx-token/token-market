//go:build unit

package zhipu

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestDeriveKey(t *testing.T) {
	for _, provider := range []Provider{ProviderBigModel, ProviderZai} {
		for _, scenario := range []string{"existing", "create", "no_secret", "no_project"} {
			t.Run(string(provider)+"/"+scenario, func(t *testing.T) {
				creates := 0
				c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
					want := "access"
					if provider == ProviderZai {
						want = "Bearer access"
					}
					if r.Header.Get("Authorization") != want || r.Header.Get("X-Client-Sig") != "" {
						t.Error("derived-key auth/signature mismatch")
					}
					switch {
					case r.URL.Path == "/api/biz/customer/getCustomerInfo":
						if scenario == "no_project" {
							respond(t, w, 0, map[string]any{"organizations": []any{}})
							return
						}
						data := `{"organizations":[{"organizationId":"ignored-team","organizationName":"默认机构","projects":[{"projectId":"team","projectType":2}]},{"organizationId":"fallback","projects":[{"projectId":"fallback"}]},{"organizationId":42,"organizationName":"我的默认机构","projects":[{"projectId":"team","projectType":"2","projectName":"默认项目"},{"projectId":"other","projectType":1},{"projectId":"selected","projectName":"我的默认项目","projectType":1}]}]}`
						var customer any
						if err := json.Unmarshal([]byte(data), &customer); err != nil {
							t.Error(err)
						}
						respond(t, w, "200", customer)
					case r.URL.Path == "/api/biz/v1/organization/42/projects/selected/api_keys":
						switch r.Method {
						case "GET":
							keys := []map[string]string{{"name": "other", "apiKey": "other-id"}}
							if scenario != "create" {
								keys = append(keys, map[string]string{"name": DerivedKeyName, "apiKey": "key-id"})
							}
							respond(t, w, 0, keys)
						case "POST":
							creates++
							var body map[string]string
							if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
								t.Error(err)
							}
							if body["name"] != DerivedKeyName {
								t.Error("bad key name")
							}
							respond(t, w, 0, map[string]string{"apiKey": "key-id"})
						default:
							t.Error("unexpected method")
						}
					case strings.HasSuffix(r.URL.Path, "/api_keys/copy/key-id"):
						secret := "secret"
						if scenario == "no_secret" {
							secret = ""
						}
						respond(t, w, 0, map[string]string{"secretKey": secret})
					default:
						t.Errorf("unexpected path %s", r.URL.Path)
						w.WriteHeader(http.StatusNotFound)
					}
				})
				key, err := c.DeriveKey(context.Background(), provider, "access")
				switch scenario {
				case "no_secret":
					if !errors.Is(err, ErrSecretUnavailable) {
						t.Fatalf("want missing secret, got %v", err)
					}
				case "no_project":
					if !errors.Is(err, ErrNoProject) {
						t.Fatalf("want missing project, got %v", err)
					}
				default:
					if err != nil {
						t.Fatal(err)
					}
					if key.Credential != "key-id.secret" || key.OrganizationID != "42" || key.ProjectID != "selected" {
						t.Fatalf("wrong derived key: %+v", key)
					}
				}
				if (creates == 1) != (scenario == "create") {
					t.Fatalf("unexpected creates %d", creates)
				}
			})
		}
	}
}

func TestSplitCredential(t *testing.T) {
	for _, credential := range []string{"", "id", "id.", ".secret", "id.a.b", " .secret", "id. ", "id.secret\n"} {
		if _, _, err := SplitCredential(credential); err == nil {
			t.Errorf("invalid credential accepted: %q", credential)
		}
	}
}
