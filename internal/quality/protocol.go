package quality

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ProbeRequest 是一次探测请求的参数。
type ProbeRequest struct {
	Model           string
	Prompt          string
	MaxTokens       int
	ReasoningEffort string
}

// ProbeResult 是一次探测的观测结果。
type ProbeResult struct {
	HTTPStatus       int
	ErrorClass       string // network / timeout / http_4xx / http_5xx / parse / empty
	Text             string
	ResponseModel    string
	OutputTokens     int
	ReasoningTokens  int
	ThinkingChars    int
	TTFT             time.Duration
	Duration         time.Duration
	Sub2APIRequestID string
}

// OK 表示请求成功拿到了输出。
func (r ProbeResult) OK() bool {
	return r.ErrorClass == "" && r.HTTPStatus >= 200 && r.HTTPStatus < 300
}

// OutputTPS 返回输出速度（token/秒，从首字算起）。
func (r ProbeResult) OutputTPS() float64 {
	gen := r.Duration - r.TTFT
	if r.OutputTokens <= 0 || gen <= 0 {
		return 0
	}
	return float64(r.OutputTokens) / gen.Seconds()
}

// protocol 把探测请求转成某种 API 协议，经由 sub2api 网关发给被测账号。
type protocol interface {
	build(req ProbeRequest) (path string, body any, stream bool)
	authHeader(h http.Header, key string)
	parseStream(r io.Reader, start time.Time, res *ProbeResult) error
	parseJSON(b []byte, res *ProbeResult) error
}

// protocolFor 返回平台对应的协议。
func protocolFor(platform string) protocol {
	switch platform {
	case "anthropic":
		return anthropicProtocol{}
	case "openai":
		return responsesProtocol{}
	case "gemini", "antigravity":
		return geminiProtocol{}
	default:
		return chatProtocol{}
	}
}

// Prober 通过 sub2api 网关发送探测请求。
type Prober struct {
	GatewayURL string
	Client     *http.Client
}

func (p *Prober) Probe(ctx context.Context, platform, key string, req ProbeRequest) ProbeResult {
	proto := protocolFor(platform)
	path, body, stream := proto.build(req)
	raw, _ := json.Marshal(body)
	start := time.Now()
	res := ProbeResult{}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.GatewayURL, "/")+path, bytes.NewReader(raw))
	if err != nil {
		res.ErrorClass = "network"
		return res
	}
	httpReq.Header.Set("Content-Type", "application/json")
	proto.authHeader(httpReq.Header, key)
	resp, err := p.Client.Do(httpReq)
	if err != nil {
		res.ErrorClass = "network"
		if ctx.Err() != nil || strings.Contains(err.Error(), "timeout") {
			res.ErrorClass = "timeout"
		}
		res.Duration = time.Since(start)
		return res
	}
	defer func() { _ = resp.Body.Close() }()
	res.HTTPStatus = resp.StatusCode
	res.Sub2APIRequestID = resp.Header.Get("X-Client-Request-ID")
	if resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		res.ErrorClass = "http_5xx"
		if resp.StatusCode < 500 {
			res.ErrorClass = "http_4xx"
		}
		res.Duration = time.Since(start)
		return res
	}
	if stream && strings.Contains(resp.Header.Get("Content-Type"), "event-stream") {
		err = proto.parseStream(resp.Body, start, &res)
	} else {
		var b []byte
		b, err = io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		if err == nil {
			res.TTFT = time.Since(start)
			err = proto.parseJSON(b, &res)
		}
	}
	res.Duration = time.Since(start)
	if err != nil {
		res.ErrorClass = "parse"
	} else if strings.TrimSpace(res.Text) == "" {
		res.ErrorClass = "empty"
	}
	return res
}

// sseEvents 逐个读取 SSE 事件，回调 (event, data)。
func sseEvents(r io.Reader, fn func(event string, data []byte) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	event := ""
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "[DONE]" {
				return nil
			}
			if err := fn(event, []byte(data)); err != nil {
				return err
			}
		case line == "":
			event = ""
		}
	}
	return sc.Err()
}

func markTTFT(res *ProbeResult, start time.Time) {
	if res.TTFT == 0 {
		res.TTFT = time.Since(start)
	}
}

// ---- Anthropic Messages ----

type anthropicProtocol struct{}

func (anthropicProtocol) build(req ProbeRequest) (string, any, bool) {
	body := map[string]any{
		"model":      req.Model,
		"max_tokens": req.MaxTokens,
		"stream":     true,
		"messages":   []map[string]any{{"role": "user", "content": req.Prompt}},
	}
	if req.ReasoningEffort != "" {
		budget := 2048
		if req.MaxTokens <= budget {
			body["max_tokens"] = budget * 2
		}
		body["thinking"] = map[string]any{"type": "enabled", "budget_tokens": budget}
	}
	return "/v1/messages", body, true
}

func (anthropicProtocol) authHeader(h http.Header, key string) {
	h.Set("x-api-key", key)
	h.Set("anthropic-version", "2023-06-01")
}

func (anthropicProtocol) parseStream(r io.Reader, start time.Time, res *ProbeResult) error {
	var text strings.Builder
	err := sseEvents(r, func(_ string, data []byte) error {
		var ev struct {
			Type    string `json:"type"`
			Message struct {
				Model string `json:"model"`
			} `json:"message"`
			Delta struct {
				Type     string `json:"type"`
				Text     string `json:"text"`
				Thinking string `json:"thinking"`
			} `json:"delta"`
			Usage struct {
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal(data, &ev) != nil {
			return nil
		}
		switch ev.Type {
		case "message_start":
			res.ResponseModel = ev.Message.Model
		case "content_block_delta":
			markTTFT(res, start)
			text.WriteString(ev.Delta.Text)
			res.ThinkingChars += len(ev.Delta.Thinking)
		case "message_delta":
			if ev.Usage.OutputTokens > 0 {
				res.OutputTokens = ev.Usage.OutputTokens
			}
		case "error":
			return fmt.Errorf("stream error: %s", data)
		}
		return nil
	})
	res.Text = text.String()
	return err
}

func (anthropicProtocol) parseJSON(b []byte, res *ProbeResult) error {
	var msg struct {
		Model   string `json:"model"`
		Content []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			Thinking string `json:"thinking"`
		} `json:"content"`
		Usage struct {
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(b, &msg); err != nil {
		return err
	}
	res.ResponseModel = msg.Model
	res.OutputTokens = msg.Usage.OutputTokens
	var text strings.Builder
	for _, c := range msg.Content {
		text.WriteString(c.Text)
		res.ThinkingChars += len(c.Thinking)
	}
	res.Text = text.String()
	return nil
}

// ---- OpenAI Responses ----

type responsesProtocol struct{}

func (responsesProtocol) build(req ProbeRequest) (string, any, bool) {
	body := map[string]any{
		"model":             req.Model,
		"input":             req.Prompt,
		"max_output_tokens": req.MaxTokens,
		"stream":            true,
	}
	if req.ReasoningEffort != "" {
		body["reasoning"] = map[string]any{"effort": req.ReasoningEffort}
	}
	return "/v1/responses", body, true
}

func (responsesProtocol) authHeader(h http.Header, key string) { h.Set("Authorization", "Bearer "+key) }

type responsesUsage struct {
	OutputTokens        int `json:"output_tokens"`
	OutputTokensDetails struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"output_tokens_details"`
}

func (responsesProtocol) parseStream(r io.Reader, start time.Time, res *ProbeResult) error {
	var text strings.Builder
	err := sseEvents(r, func(_ string, data []byte) error {
		var ev struct {
			Type     string `json:"type"`
			Delta    string `json:"delta"`
			Response struct {
				Model string         `json:"model"`
				Usage responsesUsage `json:"usage"`
			} `json:"response"`
		}
		if json.Unmarshal(data, &ev) != nil {
			return nil
		}
		switch {
		case ev.Type == "response.output_text.delta":
			markTTFT(res, start)
			text.WriteString(ev.Delta)
		case strings.HasPrefix(ev.Type, "response.reasoning"):
			markTTFT(res, start)
		case ev.Type == "response.completed" || ev.Type == "response.incomplete":
			res.ResponseModel = ev.Response.Model
			res.OutputTokens = ev.Response.Usage.OutputTokens
			res.ReasoningTokens = ev.Response.Usage.OutputTokensDetails.ReasoningTokens
		case ev.Type == "response.failed" || ev.Type == "error":
			return fmt.Errorf("stream error: %s", data)
		}
		return nil
	})
	res.Text = text.String()
	return err
}

func (responsesProtocol) parseJSON(b []byte, res *ProbeResult) error {
	var resp struct {
		Model  string `json:"model"`
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		Usage responsesUsage `json:"usage"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return err
	}
	res.ResponseModel = resp.Model
	res.OutputTokens = resp.Usage.OutputTokens
	res.ReasoningTokens = resp.Usage.OutputTokensDetails.ReasoningTokens
	var text strings.Builder
	for _, o := range resp.Output {
		for _, c := range o.Content {
			if c.Type == "output_text" {
				text.WriteString(c.Text)
			}
		}
	}
	res.Text = text.String()
	return nil
}

// ---- OpenAI Chat Completions（Grok、国产 Coding Plan）----

type chatProtocol struct{}

func (chatProtocol) build(req ProbeRequest) (string, any, bool) {
	body := map[string]any{
		"model":          req.Model,
		"max_tokens":     req.MaxTokens,
		"stream":         true,
		"stream_options": map[string]any{"include_usage": true},
		"messages":       []map[string]any{{"role": "user", "content": req.Prompt}},
	}
	if req.ReasoningEffort != "" {
		body["reasoning_effort"] = req.ReasoningEffort
	}
	return "/v1/chat/completions", body, true
}

func (chatProtocol) authHeader(h http.Header, key string) { h.Set("Authorization", "Bearer "+key) }

type chatUsage struct {
	CompletionTokens        int `json:"completion_tokens"`
	CompletionTokensDetails struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

func (chatProtocol) parseStream(r io.Reader, start time.Time, res *ProbeResult) error {
	var text strings.Builder
	err := sseEvents(r, func(_ string, data []byte) error {
		var ev struct {
			Model   string `json:"model"`
			Choices []struct {
				Delta struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
				} `json:"delta"`
			} `json:"choices"`
			Usage *chatUsage `json:"usage"`
		}
		if json.Unmarshal(data, &ev) != nil {
			return nil
		}
		if ev.Model != "" {
			res.ResponseModel = ev.Model
		}
		for _, c := range ev.Choices {
			if c.Delta.Content != "" || c.Delta.ReasoningContent != "" {
				markTTFT(res, start)
			}
			text.WriteString(c.Delta.Content)
			res.ThinkingChars += len(c.Delta.ReasoningContent)
		}
		if ev.Usage != nil {
			res.OutputTokens = ev.Usage.CompletionTokens
			res.ReasoningTokens = ev.Usage.CompletionTokensDetails.ReasoningTokens
		}
		return nil
	})
	res.Text = text.String()
	return err
}

func (chatProtocol) parseJSON(b []byte, res *ProbeResult) error {
	var resp struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"message"`
		} `json:"choices"`
		Usage chatUsage `json:"usage"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return err
	}
	res.ResponseModel = resp.Model
	res.OutputTokens = resp.Usage.CompletionTokens
	res.ReasoningTokens = resp.Usage.CompletionTokensDetails.ReasoningTokens
	if len(resp.Choices) > 0 {
		res.Text = resp.Choices[0].Message.Content
		res.ThinkingChars = len(resp.Choices[0].Message.ReasoningContent)
	}
	return nil
}

// ---- Gemini（Gemini、Antigravity）----

type geminiProtocol struct{}

func (geminiProtocol) build(req ProbeRequest) (string, any, bool) {
	cfg := map[string]any{"maxOutputTokens": req.MaxTokens}
	if req.ReasoningEffort != "" {
		cfg["thinkingConfig"] = map[string]any{"includeThoughts": false, "thinkingBudget": 2048}
	}
	body := map[string]any{
		"contents":         []map[string]any{{"role": "user", "parts": []map[string]any{{"text": req.Prompt}}}},
		"generationConfig": cfg,
	}
	return "/v1beta/models/" + req.Model + ":generateContent", body, false
}

func (geminiProtocol) authHeader(h http.Header, key string) { h.Set("x-goog-api-key", key) }

func (geminiProtocol) parseStream(io.Reader, time.Time, *ProbeResult) error {
	return fmt.Errorf("gemini probe uses non-streaming requests")
}

func (geminiProtocol) parseJSON(b []byte, res *ProbeResult) error {
	var resp struct {
		ModelVersion string `json:"modelVersion"`
		Candidates   []struct {
			Content struct {
				Parts []struct {
					Text    string `json:"text"`
					Thought bool   `json:"thought"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		UsageMetadata struct {
			CandidatesTokenCount int `json:"candidatesTokenCount"`
			ThoughtsTokenCount   int `json:"thoughtsTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return err
	}
	res.ResponseModel = resp.ModelVersion
	res.OutputTokens = resp.UsageMetadata.CandidatesTokenCount
	res.ReasoningTokens = resp.UsageMetadata.ThoughtsTokenCount
	var text strings.Builder
	if len(resp.Candidates) > 0 {
		for _, p := range resp.Candidates[0].Content.Parts {
			if !p.Thought {
				text.WriteString(p.Text)
			}
		}
	}
	res.Text = text.String()
	return nil
}
