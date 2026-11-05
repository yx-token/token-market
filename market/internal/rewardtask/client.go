// Package rewardtask 把闲猫接入 loxo 任务中心（合作站点侧）：落地票据、注册 / 充值回调、浏览任务 SDK。
// 协议与状态机见 docs/2026-11-06-loxo-reward-tasks.md。
package rewardtask

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	openPrefix       = "/api/v1/reward-open"
	maxResponseBytes = 1 << 20
)

// APIError 是 loxo 开放接口返回的失败（HTTP 非 2xx 或 code != 0）。
type APIError struct {
	Status  int
	Reason  string // REWARD_*；代理返回的 HTML 错误页等没有 reason
	Message string
}

func (e *APIError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("loxo %d %s: %s", e.Status, e.Reason, e.Message)
	}
	return fmt.Sprintf("loxo %d: %s", e.Status, e.Message)
}

// Client 调用 loxo 的签名接口（ping / tickets/query / events）。
type Client struct {
	BaseURL string
	AppKey  string
	Secret  string
	HTTP    *http.Client
	Now     func() time.Time
}

// Sign 计算 X-Reward-Signature：hex_lower(HMAC_SHA256(secret, timestamp + "\n" + nonce + "\n" + body))。
func Sign(secret, timestamp, nonce string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp + "\n" + nonce + "\n"))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func newNonce() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

type envelope struct {
	Code    *int            `json:"code"`
	Message string          `json:"message"`
	Reason  string          `json:"reason"`
	Data    json.RawMessage `json:"data"`
}

// call 签名并 POST；成功时把 data 解到 out，返回 data 原文。每次调用都生成新的 timestamp / nonce，重试安全。
func (c *Client) call(ctx context.Context, path string, body []byte, out any) (json.RawMessage, error) {
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	nonce, err := newNonce()
	if err != nil {
		return nil, err
	}
	ts := strconv.FormatInt(now().Unix(), 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+openPrefix+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Reward-App-Key", c.AppKey)
	req.Header.Set("X-Reward-Timestamp", ts)
	req.Header.Set("X-Reward-Nonce", nonce)
	req.Header.Set("X-Reward-Signature", Sign(c.Secret, ts, nonce, body))
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, err
	}
	var env envelope
	if json.Unmarshal(raw, &env) != nil || env.Code == nil {
		return nil, &APIError{Status: resp.StatusCode, Message: fmt.Sprintf("unexpected response (HTTP %d)", resp.StatusCode)}
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 || *env.Code != 0 {
		status := resp.StatusCode
		if status >= 200 && status <= 299 {
			status = *env.Code
		}
		return nil, &APIError{Status: status, Reason: env.Reason, Message: env.Message}
	}
	if out != nil {
		if err := json.Unmarshal(env.Data, out); err != nil {
			return nil, &APIError{Status: resp.StatusCode, Message: "decode data: " + err.Error()}
		}
	}
	return env.Data, nil
}

type PingResult struct {
	PartnerName string `json:"partner_name"`
	AppKey      string `json:"app_key"`
	ServerTime  int64  `json:"server_time"`
}

func (c *Client) Ping(ctx context.Context) (*PingResult, error) {
	var out PingResult
	if _, err := c.call(ctx, "/ping", []byte("{}"), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// TicketView 是 tickets/query 的 data。
type TicketView struct {
	Ticket            string    `json:"ticket"`
	TaskID            int64     `json:"task_id"`
	TaskTitle         string    `json:"task_title"`
	TaskType          string    `json:"task_type"`
	Period            string    `json:"period"`
	PeriodKey         string    `json:"period_key"`
	Status            string    `json:"status"`
	ExpiresAt         time.Time `json:"expires_at"`
	Expired           bool      `json:"expired"`
	ExternalUserID    string    `json:"external_user_id"`
	BrowseSeconds     int       `json:"browse_seconds"`
	ProgressSeconds   int       `json:"progress_seconds"`
	MinRechargeAmount float64   `json:"min_recharge_amount"`
	RechargeTotal     float64   `json:"recharge_total"`
}

func (c *Client) QueryTicket(ctx context.Context, ticket string) (*TicketView, error) {
	body, err := json.Marshal(map[string]string{"ticket": ticket})
	if err != nil {
		return nil, err
	}
	var out TicketView
	if _, err := c.call(ctx, "/tickets/query", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// EventInput 是 events 的请求体。amount 用字符串传，避免浮点误差（loxo 接受数字字符串）。
type EventInput struct {
	EventID        string `json:"event_id"`
	EventType      string `json:"event_type"`
	Ticket         string `json:"ticket"`
	ExternalUserID string `json:"external_user_id"`
	Amount         string `json:"amount,omitempty"`
	OrderID        string `json:"order_id,omitempty"`
	OccurredAt     int64  `json:"occurred_at,omitempty"`
}

type EventResult struct {
	EventID   string `json:"event_id"`
	Duplicate bool   `json:"duplicate"`
	Result    string `json:"result"` // accepted / completed / ignored
	TaskID    int64  `json:"task_id"`
	TaskType  string `json:"task_type"`
	Status    string `json:"status"` // in_progress / completed
	Rewarded  bool   `json:"rewarded"`
}

func (c *Client) SendEvent(ctx context.Context, in EventInput) (*EventResult, json.RawMessage, error) {
	body, err := json.Marshal(in)
	if err != nil {
		return nil, nil, err
	}
	var out EventResult
	raw, err := c.call(ctx, "/events", body, &out)
	if err != nil {
		return nil, nil, err
	}
	return &out, raw, nil
}

// reasonOf 取 loxo 的 reason；网络错误等返回空串。
func reasonOf(err error) string {
	var ae *APIError
	if errors.As(err, &ae) {
		return ae.Reason
	}
	return ""
}
