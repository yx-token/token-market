package listing

import "testing"

func TestAccountEmail(t *testing.T) {
	cases := []struct {
		name string
		tok  map[string]any
		want string
	}{
		{"claude", map[string]any{"account_uuid": "acct-1", "email_address": " alice@example.com "}, "alice@example.com"},
		{"openai", map[string]any{"chatgpt_account_id": "acct-2", "email": "bob@example.com"}, "bob@example.com"},
		{"claude 优先 email_address", map[string]any{"email_address": "a@example.com", "email": "b@example.com"}, "a@example.com"},
		{"email_address 无效时取 email", map[string]any{"email_address": "", "email": "b@example.com"}, "b@example.com"},
		{"gemini 没有邮箱", map[string]any{"project_id": "proj-1", "tier_id": "google_ai_pro"}, ""},
		{"不是邮箱", map[string]any{"email": "not-an-email"}, ""},
		{"非字符串", map[string]any{"email": 42}, ""},
		{"空", nil, ""},
	}
	for _, c := range cases {
		if got := AccountEmail(c.tok); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
