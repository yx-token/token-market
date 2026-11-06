package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultResendAPI = "https://api.resend.com/emails"

// Resend 通过 https://resend.com API 发信。域名需在 Resend 后台验证。
type Resend struct {
	APIKey string
	From   string
	APIURL string // 测试可覆盖；空则用官方地址
	Client *http.Client
}

func NewResend(apiKey, from string) *Resend {
	from = strings.TrimSpace(from)
	if from == "" {
		from = "闲猫 <onboarding@resend.dev>"
	}
	return &Resend{
		APIKey: strings.TrimSpace(apiKey),
		From:   from,
		Client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (r *Resend) Send(ctx context.Context, to, subject, html string) error {
	if r == nil || r.APIKey == "" {
		return ErrNotConfigured
	}
	body, err := json.Marshal(map[string]any{
		"from":    r.From,
		"to":      []string{to},
		"subject": subject,
		"html":    html,
	})
	if err != nil {
		return err
	}
	api := r.APIURL
	if api == "" {
		api = defaultResendAPI
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, api, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+r.APIKey)
	req.Header.Set("Content-Type", "application/json")
	client := r.Client
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("resend: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 4096))
	if err != nil {
		return fmt.Errorf("resend: read body: %w", err)
	}
	if res.StatusCode >= 300 {
		return fmt.Errorf("resend: HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}
