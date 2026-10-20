// Package fakebazaar 是 BazaarLink Probe API 的替身，用于测试与本地开发：按真实接口收单、异步检测、轮询结果。
// 检测时真的会向被测端点发几道探针题，根据回答里自称的模型判定身份是否一致。不能用于生产。
package fakebazaar

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yx-token/idlicat/market/internal/platform/randx"
)

type item struct {
	ProbeID      string `json:"probeId"`
	Label        string `json:"label"`
	Group        string `json:"group"`
	Neutral      bool   `json:"neutral"`
	Status       string `json:"status"`
	Passed       any    `json:"passed"`
	PassReason   string `json:"passReason,omitempty"`
	Error        string `json:"error,omitempty"`
	DurationMs   *int64 `json:"durationMs"`
	InputTokens  *int   `json:"inputTokens"`
	OutputTokens *int   `json:"outputTokens"`
	Response     string `json:"response,omitempty"`
	prompt       string
}

type run struct {
	RunID              string         `json:"runId"`
	BaseURL            string         `json:"baseUrl"`
	UpstreamFormat     string         `json:"upstreamFormat"`
	ProtocolPath       string         `json:"protocolPath"`
	ModelID            string         `json:"modelId"`
	ClaimedModel       string         `json:"claimedModel,omitempty"`
	Status             string         `json:"status"`
	Items              []*item        `json:"items"`
	CreatedAt          int64          `json:"createdAt"`
	CompletedAt        *int64         `json:"completedAt"`
	WallClockMs        *int64         `json:"wallClockMs,omitempty"`
	TotalInputTokens   *int           `json:"totalInputTokens"`
	TotalOutputTokens  *int           `json:"totalOutputTokens"`
	IdentityAssessment map[string]any `json:"identityAssessment"`
	Warnings           []any          `json:"warnings"`
	ReportURL          string         `json:"reportUrl,omitempty"`
	Score              any            `json:"score"`
	StartedByIP        string         `json:"startedByIp"`
	apiKey             string
	done               chan struct{}
}

type Server struct {
	HTTP *httptest.Server
	h    http.Handler

	mu        sync.Mutex
	runs      map[string]*run
	submitted []time.Time
	// LimitPerHour 是每小时允许的提交次数，0 表示不限（真实服务为 30）
	LimitPerHour int
	// Delay 是每道题之间的等待，模拟检测耗时
	Delay  time.Duration
	client *http.Client
	keys   []string
}

func New() *Server {
	s := NewUnstarted()
	s.HTTP = httptest.NewServer(s.h)
	return s
}

func NewUnstarted() *Server {
	s := &Server{runs: map[string]*run{}, client: &http.Client{Timeout: 2 * time.Minute}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/probe/run", s.submit)
	mux.HandleFunc("GET /api/probe/run/{id}", s.get)
	mux.HandleFunc("GET /api/probe/report/{id}", s.report)
	s.h = mux
	return s
}

func (s *Server) Handler() http.Handler { return s.h }
func (s *Server) URL() string           { return s.HTTP.URL }
func (s *Server) Close() {
	if s.HTTP != nil {
		s.HTTP.Close()
	}
}

// APIKeys 返回收到过的被测端点 key（测试用来确认临时令牌已失效）。
func (s *Server) APIKeys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.keys...)
}

// Wait 等待某次检测结束。
func (s *Server) Wait(id string) {
	s.mu.Lock()
	r := s.runs[id]
	s.mu.Unlock()
	if r != nil {
		<-r.done
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

var probes = []struct{ id, label, group, prompt string }{
	{"self_identity", "自我认知", "identity", "Which model are you? Reply with your exact model id only."},
	{"vendor_identity", "开发方", "identity", "Which company trained you? Answer in one short sentence and include your model name."},
	{"arith", "基础推理", "integrity", "What is 17 * 23? Reply with only the number."},
	{"instruction", "指令遵循", "integrity", "Reply with exactly the word READY and nothing else."},
}

func (s *Server) submit(w http.ResponseWriter, r *http.Request) {
	var in struct {
		BaseURL        string `json:"baseUrl"`
		APIKey         string `json:"apiKey"`
		ModelID        string `json:"modelId"`
		ClaimedModel   string `json:"claimedModel"`
		UpstreamFormat string `json:"upstreamFormat"`
		Sync           bool   `json:"sync"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	if in.BaseURL == "" || in.APIKey == "" || in.ModelID == "" {
		writeJSON(w, 400, map[string]string{"error": "baseUrl, apiKey and modelId are required"})
		return
	}
	if in.UpstreamFormat == "" {
		in.UpstreamFormat = "openai"
	}
	s.mu.Lock()
	now := time.Now()
	var recent []time.Time
	for _, t := range s.submitted {
		if now.Sub(t) < time.Hour {
			recent = append(recent, t)
		}
	}
	s.submitted = recent
	if s.LimitPerHour > 0 && len(recent) >= s.LimitPerHour {
		retry := int(time.Hour.Seconds() - now.Sub(recent[0]).Seconds())
		s.mu.Unlock()
		w.Header().Set("Retry-After", strconv.Itoa(retry))
		writeJSON(w, 429, map[string]any{"error": "rate limited", "retryAfterSeconds": retry})
		return
	}
	s.submitted = append(s.submitted, now)
	rn := &run{RunID: randx.String(8) + "-" + randx.String(4) + "-" + randx.String(12), BaseURL: in.BaseURL, UpstreamFormat: in.UpstreamFormat,
		ModelID: in.ModelID, ClaimedModel: in.ClaimedModel, Status: "running", CreatedAt: now.UnixMilli(), Warnings: []any{},
		StartedByIP: "203.0.113.1", apiKey: in.APIKey, done: make(chan struct{})}
	rn.ProtocolPath = "/v1/chat/completions"
	if in.UpstreamFormat == "anthropic" {
		rn.ProtocolPath = "/v1/messages"
	}
	for _, p := range probes {
		rn.Items = append(rn.Items, &item{ProbeID: p.id, Label: p.label, Group: p.group, Status: "pending", prompt: p.prompt})
	}
	s.runs[rn.RunID] = rn
	s.keys = append(s.keys, in.APIKey)
	s.mu.Unlock()
	go s.execute(rn)
	if in.Sync {
		<-rn.done
		s.mu.Lock()
		defer s.mu.Unlock()
		writeJSON(w, 200, rn)
		return
	}
	writeJSON(w, 200, map[string]string{"runId": rn.RunID})
}

func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rn := s.runs[r.PathValue("id")]
	if rn == nil {
		writeJSON(w, 404, map[string]string{"error": "run not found"})
		return
	}
	writeJSON(w, 200, rn)
}

var reportTmpl = template.Must(template.New("r").Parse(`<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><title>Probe report</title></head>
<body style="font-family:system-ui;max-width:640px;margin:40px auto"><h1>BazaarLink Probe（开发替身）</h1>
<p>模型：{{.ModelID}}　状态：{{.Status}}</p><p id="verdict">身份判定：{{.Verdict}}</p>
<ul>{{range .Items}}<li>{{.Label}}：{{.Status}} {{.PassReason}}{{.Error}}</li>{{end}}</ul></body></html>`))

func (s *Server) report(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	rn := s.runs[r.PathValue("id")]
	s.mu.Unlock()
	if rn == nil {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	verdict := "—"
	if v, ok := rn.IdentityAssessment["verdict"].(map[string]any); ok {
		verdict = fmt.Sprint(v["status"])
	}
	data := map[string]any{"ModelID": rn.ModelID, "Status": rn.Status, "Verdict": verdict, "Items": rn.Items}
	s.mu.Unlock()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = reportTmpl.Execute(w, data)
}

// call 按协议向被测端点发一道题，返回回答文本、响应中的模型名与 token 数。
func (s *Server) call(rn *run, prompt string) (string, string, int, int, error) {
	var body map[string]any
	var path string
	if rn.UpstreamFormat == "anthropic" {
		path = "/messages"
		body = map[string]any{"model": rn.ModelID, "max_tokens": 256, "messages": []map[string]string{{"role": "user", "content": prompt}}}
	} else {
		path = "/chat/completions"
		body = map[string]any{"model": rn.ModelID, "max_tokens": 256, "messages": []map[string]string{{"role": "user", "content": prompt}}}
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(rn.BaseURL, "/")+path, bytes.NewReader(raw))
	if err != nil {
		return "", "", 0, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if rn.UpstreamFormat == "anthropic" {
		req.Header.Set("x-api-key", rn.apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	} else {
		req.Header.Set("Authorization", "Bearer "+rn.apiKey)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return "", "", 0, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != 200 {
		return "", "", 0, 0, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(data), 200))
	}
	var out struct {
		Model   string `json:"model"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage map[string]int `json:"usage"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", "", 0, 0, fmt.Errorf("invalid JSON response")
	}
	text := ""
	if len(out.Content) > 0 {
		text = out.Content[0].Text
	} else if len(out.Choices) > 0 {
		text = out.Choices[0].Message.Content
	}
	in := out.Usage["input_tokens"] + out.Usage["prompt_tokens"]
	outTok := out.Usage["output_tokens"] + out.Usage["completion_tokens"]
	return text, out.Model, in, outTok, nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

var modelRe = regexp.MustCompile(`(?i)\b(claude-[a-z0-9.\-]+|gpt-[a-z0-9.\-]+|gemini-[a-z0-9.\-]+|glm-[a-z0-9.\-]+|kimi-[a-z0-9.\-]+|grok-[a-z0-9.\-]+|deepseek-[a-z0-9.\-]+|qwen[a-z0-9.\-]*|minimax-[a-z0-9.\-]+)`)

func family(model string) string {
	m := strings.ToLower(model)
	for _, f := range []struct{ key, name string }{{"claude", "claude"}, {"gpt", "gpt"}, {"codex", "gpt"}, {"gemini", "gemini"}, {"glm", "glm"},
		{"kimi", "kimi"}, {"grok", "grok"}, {"deepseek", "deepseek"}, {"qwen", "qwen"}, {"minimax", "minimax"}} {
		if strings.Contains(m, f.key) {
			return f.name
		}
	}
	return ""
}

func (s *Server) execute(rn *run) {
	defer close(rn.done)
	start := time.Now()
	var texts []string
	ok, totalIn, totalOut := 0, 0, 0
	for _, it := range rn.Items {
		if s.Delay > 0 {
			time.Sleep(s.Delay)
		}
		t0 := time.Now()
		text, _, in, out, err := s.call(rn, it.prompt)
		d := time.Since(t0).Milliseconds()
		s.mu.Lock()
		it.DurationMs = &d
		if err != nil {
			it.Status, it.Error = "error", err.Error()
		} else {
			ok++
			totalIn += in
			totalOut += out
			it.Status, it.Passed, it.Response = "done", true, truncate(text, 200)
			it.InputTokens, it.OutputTokens = &in, &out
			texts = append(texts, text)
		}
		s.mu.Unlock()
	}
	claimed := rn.ClaimedModel
	if claimed == "" {
		claimed = rn.ModelID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UnixMilli()
	wall := time.Since(start).Milliseconds()
	rn.Status, rn.CompletedAt, rn.WallClockMs = "completed", &now, &wall
	rn.TotalInputTokens, rn.TotalOutputTokens = &totalIn, &totalOut
	rn.ReportURL = "/api/probe/report/" + rn.RunID
	if ok == 0 {
		return // 与真实服务一致：请求全部失败时没有身份评估
	}
	outer, status, trueModel := "match", "clean_match", claimed
	for _, t := range texts {
		for _, m := range modelRe.FindAllString(t, -1) {
			m = strings.ToLower(strings.TrimRight(m, ".-"))
			switch {
			case m == strings.ToLower(claimed):
			case family(m) != family(claimed):
				outer, status, trueModel = "mismatch", "plain_mismatch", m
			case status == "clean_match":
				status, trueModel = "clean_match_submodel_mismatch", m
			}
		}
	}
	rn.IdentityAssessment = map[string]any{
		"status": outer, "claimedModel": claimed, "predictedFamily": family(trueModel), "confidence": 0.9, "riskFlags": []string{},
		"evidence": []any{}, "verdict": map[string]any{"status": status, "trueFamily": family(trueModel), "trueModel": trueModel,
			"spoofMethod": nil, "confidence": "high", "reasoning": "fake detector: based on the model names the endpoint claims"},
	}
	if status != "clean_match" {
		rn.Warnings = append(rn.Warnings, map[string]any{"id": "identity_claim", "label": "自称模型与声明不一致", "severity": "warning", "evidence": trueModel})
	}
}
