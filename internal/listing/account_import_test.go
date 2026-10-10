package listing

import (
	"encoding/json"
	"testing"
)

func TestParseImportedAccount(t *testing.T) {
	account := `{"name":"seller account","platform":"anthropic","type":"oauth","credentials":{"access_token":"secret-token"},"priority":1,"group_ids":[8]}`
	for _, raw := range []string{
		account, "[" + account + "]", `{"type":"sub2api-data","version":1,"proxies":[],"accounts":[` + account + `]}`,
		`{"type":"sub2api-bundle","version":1,"accounts":[` + account + `]}`,
		`{"data":{"accounts":[` + account + `]}}`, `{"access_token":"secret-token"}`,
	} {
		a, err := parseImportedAccount(ImportAccountInput{Data: json.RawMessage(raw)})
		if err != nil || a.Credentials["access_token"] != "secret-token" {
			t.Fatalf("supported format rejected: %v", err)
		}
	}
	a, err := parseImportedAccount(ImportAccountInput{Data: json.RawMessage(`[null,` + account + `]`), AccountIndex: 1})
	if err != nil || a.Name != "seller account" {
		t.Fatalf("account selection: %v", err)
	}
	for _, raw := range []string{"", "null", "[]", "true", "{", `{"accounts":null}`, `{"type":"other","accounts":[` + account + `]}`, `{"version":2,"accounts":[` + account + `]}`, `{"platform":"anthropic"}`, `{}`} {
		if _, err := parseImportedAccount(ImportAccountInput{Data: json.RawMessage(raw)}); err == nil {
			t.Fatalf("bad data accepted: %s", raw)
		}
	}
	for _, index := range []int{-1, 1} {
		if _, err := parseImportedAccount(ImportAccountInput{Data: json.RawMessage(account), AccountIndex: index}); err == nil {
			t.Fatal("invalid account index accepted")
		}
	}
}

func TestImportedAccountValidation(t *testing.T) {
	for _, tc := range []struct {
		raw, platform, auth string
		valid               bool
	}{
		{`{"access_token":" token "}`, "anthropic", "oauth", true},
		{`{"access_token":123}`, "anthropic", "oauth", false},
		{`{"refresh_token":"refresh"}`, "openai", "oauth", false},
		{`{"platform":"openai","credentials":{"access_token":"t"}}`, "anthropic", "oauth", false},
		{`{"type":"setup-token","credentials":{"access_token":"t"}}`, "anthropic", "oauth", false},
		{`{"type":"setup-token","credentials":{"access_token":"t"}}`, "anthropic", "setup-token", true},
		{`{"type":"apikey","credentials":{"api_key":"sk-coding"}}`, "kimi", "coding", true},
		{`{"access_token":"t","oauth_type":"google_one"}`, "gemini", "oauth", false},
		{`{"access_token":"t","expires_at":1900000000}`, "gemini", "google_one", true},
	} {
		a, err := parseImportedAccount(ImportAccountInput{Data: json.RawMessage(tc.raw)})
		if err != nil {
			t.Fatal(err)
		}
		if err = a.validate(tc.platform, tc.auth); (err == nil) != tc.valid {
			t.Fatalf("validation platform=%s auth=%s: %v", tc.platform, tc.auth, err)
		}
	}
}
