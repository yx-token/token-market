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
	Model           string            `json:"model,omitempty"`
	Prompt          string            `json:"prompt,omitempty"`
	MaxTokens       int               `json:"max_tokens,omitempty"`
	ReasoningEffort string            `json:"reasoning_effort,omitempty"`
	System          string            `json:"system,omitempty"`
	Messages        []ProbeMessage    `json:"messages,omitempty"`
	Tools           []ProbeTool       `json:"tools,omitempty"`
	Attachments     []ProbeAttachment `json:"attachments,omitempty"`
	Effort          string            `json:"effort,omitempty"`
	Extra           map[string]any    `json:"extra,omitempty"`
	Expected        ProbeExpected     `json:"expected,omitempty"`
	Steps           []ProbeStep       `json:"steps,omitempty"`
}

// ProbeResult 是一次探测的观测结果；可选计数用 nil 区分没有观测。
type ProbeResult struct {
	HTTPStatus          int
	ErrorClass          string
	Text                string
	ResponseModel       string
	OutputTokens        int
	ReasoningTokens     int
	ThinkingChars       int // 仅保留旧字段兼容，不用于 Claude 推理量估算。
	TTFT                time.Duration
	Duration            time.Duration
	Sub2APIRequestID    string
	InputTokens         *int64          `json:"input_tokens,omitempty"`
	CacheReadTokens     *int64          `json:"cache_read_tokens,omitempty"`
	CacheCreationTokens *int64          `json:"cache_creation_tokens,omitempty"`
	StopReason          string          `json:"stop_reason,omitempty"`
	Effort              string          `json:"effort,omitempty"`
	ToolCalls           []ProbeToolCall `json:"tool_calls,omitempty"`
	ErrorBody           string          `json:"error_body,omitempty"`
	ErrorCode           string          `json:"error_code,omitempty"`
	Flags               []string        `json:"flags,omitempty"`
	RetryAfter          time.Duration   `json:"retry_after,omitempty"`
	ReasoningObserved   bool            `json:"reasoning_observed,omitempty"`
	OutputObserved      bool            `json:"output_observed,omitempty"`
}

// OK 表示请求成功拿到了协议输出；拒答与截断另由 flags 表达。
func (r ProbeResult) OK() bool {
	return r.ErrorClass == "" && r.HTTPStatus >= 200 && r.HTTPStatus < 300
}

func (r ProbeResult) OutputTPS() float64 {
	gen := r.Duration - r.TTFT
	if r.OutputTokens <= 0 || gen <= 0 {
		return 0
	}
	return float64(r.OutputTokens) / gen.Seconds()
}

type protocol interface {
	build(req ProbeRequest) (path string, body any, stream bool)
	authHeader(h http.Header, key string)
	parseStream(r io.Reader, start time.Time, res *ProbeResult) error
	parseJSON(b []byte, res *ProbeResult) error
}

func protocolFor(platform string) protocol {
	switch platform {
	case "anthropic":
		return anthropicProtocol{}
	case "openai":
		return responsesProtocol{}
	case "antigravity":
		return geminiProtocol{}
	case "gemini":
		return geminiProtocol{}
	default:
		return chatProtocol{}
	}
}

func protocolForModel(platform, model string) protocol {
	if platform == "antigravity" && strings.HasPrefix(strings.ToLower(model), "claude-") {
		return anthropicProtocol{}
	}
	return protocolFor(platform)
}

// Prober 只通过 sub2api 网关发送探测请求。
type Prober struct {
	GatewayURL string
	Client     *http.Client
}

func (p *Prober) Probe(ctx context.Context, platform, key string, req ProbeRequest) ProbeResult {
	start := time.Now()
	res := ProbeResult{Effort: req.effectiveEffort()}
	if err := validateProbeRequest(platform, req); err != nil {
		res.ErrorClass, res.ErrorCode, res.ErrorBody = "configuration", "invalid_probe_request", err.Error()
		return res
	}
	proto := protocolForModel(platform, req.Model)
	path, body, stream := proto.build(req)
	raw, err := json.Marshal(body)
	if err != nil {
		res.ErrorClass, res.ErrorCode, res.ErrorBody = "configuration", "invalid_probe_request", "探测请求无法编码为 JSON"
		return res
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.GatewayURL, "/")+path, bytes.NewReader(raw))
	if err != nil {
		res.ErrorClass = "network"
		return res
	}
	httpReq.Header.Set("Content-Type", "application/json")
	proto.authHeader(httpReq.Header, key)
	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(httpReq)
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
	res.RetryAfter = parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		classifyProbeError(&res, b, req, key)
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
		if res.ErrorBody != "" {
			classifyProbeError(&res, []byte(res.ErrorBody), req, key)
		} else if ctx.Err() != nil {
			res.ErrorClass = "timeout"
		} else if res.ErrorClass == "" {
			res.ErrorClass = "parse"
		}
		res.addFlag("stop_error")
	} else {
		normalizeStop(&res)
		if strings.TrimSpace(res.Text) == "" && len(res.ToolCalls) == 0 && !res.HasFlag("refusal") {
			res.ErrorClass = "empty"
		}
	}
	if platform == "anthropic" || (platform == "antigravity" && strings.HasPrefix(req.Model, "claude-")) {
		estimateClaudeReasoning(req, &res)
	}
	return res
}

// sseEvents 按空行合并多行 data，并将 DONE 作为可观测的终止事件。
func sseEvents(r io.Reader, fn func(event string, data []byte) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	event := ""
	var data []string
	flush := func() error {
		if len(data) == 0 {
			event = ""
			return nil
		}
		joined := strings.Join(data, "\n")
		data = nil
		if joined == "[DONE]" {
			event = "done"
		}
		err := fn(event, []byte(joined))
		event = ""
		return err
	}
	for sc.Scan() {
		line := strings.TrimSuffix(sc.Text(), "\r")
		switch {
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		case line == "":
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return flush()
}

func markTTFT(res *ProbeResult, start time.Time) {
	if res.TTFT == 0 {
		res.TTFT = time.Since(start)
	}
}

func requireComplete(complete bool) error {
	if !complete {
		return fmt.Errorf("流式响应缺少结束事件")
	}
	return nil
}
