package thirdparty

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// BazaarLink 的 Probe API：向被测端点发送约 80 道探针题，给出模型身份判定（是否为声明的模型、是否伪装）与行为警告。
// 文档：https://bazaarlink.ai/probe 。提交频率限制为同一 IP 每小时 30 次；检测完成后结果保留 30 分钟。
func init() {
	register(Factory{
		Name:           "bazaarlink",
		Label:          "BazaarLink",
		Description:    "模型身份检测：判断端点背后是否为声明的模型（家族与具体型号），识别伪装与掉包，并给出行为警告。",
		DocsURL:        "https://bazaarlink.ai/probe",
		DefaultBaseURL: "https://bazaarlink.ai",
		Options: map[string]string{
			"run_context_check":   "是否执行上下文长度检查（默认开启）",
			"bias_fingerprint":    "是否执行指纹探针（默认开启）",
			"client_mimicry":      "模拟客户端指纹，例如 claude-code（默认不模拟）",
			"lang":                "探针语言提示，例如 zh",
			"effort_verification": "是否检查推理强度（部分模型不支持）",
		},
		RunsPerHourLimit: 30,
		RequestsPerRun:   90,
		New: func(cfg Config, client *http.Client) Provider {
			return &bazaarLink{base: strings.TrimRight(cfg.BaseURL, "/"), client: client, opts: cfg.Options}
		},
	})
}

type bazaarLink struct {
	base   string
	client *http.Client
	opts   map[string]any
}

func (b *bazaarLink) Name() string { return "bazaarlink" }

func (b *bazaarLink) Submit(ctx context.Context, t Target) (string, error) {
	body := map[string]any{
		"baseUrl":        t.BaseURL,
		"apiKey":         t.APIKey,
		"modelId":        t.Model,
		"claimedModel":   t.ClaimedModel,
		"upstreamFormat": t.Format,
		"sync":           false,
	}
	for opt, field := range map[string]string{"run_context_check": "runContextCheck", "bias_fingerprint": "biasFingerprint",
		"effort_verification": "effortVerification"} {
		if v, ok := b.opts[opt].(bool); ok {
			body[field] = v
		}
	}
	for opt, field := range map[string]string{"client_mimicry": "clientMimicry", "lang": "lang"} {
		if v, ok := b.opts[opt].(string); ok && v != "" {
			body[field] = v
		}
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.base+"/api/probe/run", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("bazaarlink submit: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusTooManyRequests {
		return "", &RateLimitedError{RetryAfter: retryAfter(resp.Header, data)}
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("bazaarlink submit: http %d: %s", resp.StatusCode, errorText(data))
	}
	var out struct {
		RunID string `json:"runId"`
	}
	if err := json.Unmarshal(data, &out); err != nil || out.RunID == "" {
		return "", fmt.Errorf("bazaarlink submit: unexpected response: %s", truncate(string(data), 200))
	}
	return out.RunID, nil
}

func retryAfter(h http.Header, body []byte) time.Duration {
	var v struct {
		RetryAfterSeconds float64 `json:"retryAfterSeconds"`
	}
	if json.Unmarshal(body, &v) == nil && v.RetryAfterSeconds > 0 {
		return time.Duration(v.RetryAfterSeconds * float64(time.Second))
	}
	if s, err := strconv.Atoi(h.Get("Retry-After")); err == nil && s > 0 {
		return time.Duration(s) * time.Second
	}
	return 5 * time.Minute
}

func errorText(body []byte) string {
	var v struct {
		Error   any    `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &v) == nil {
		switch e := v.Error.(type) {
		case string:
			return truncate(e, 300)
		case map[string]any:
			if m, ok := e["message"].(string); ok {
				return truncate(m, 300)
			}
		}
		if v.Message != "" {
			return truncate(v.Message, 300)
		}
	}
	return truncate(string(body), 300)
}

// bazaarRun 只解析需要的字段；响应中的被测端点原始回答、响应头、提交者 IP 等不保存。
type bazaarRun struct {
	RunID             string       `json:"runId"`
	Status            string       `json:"status"`
	ModelID           string       `json:"modelId"`
	ClaimedModel      string       `json:"claimedModel"`
	ProtocolPath      string       `json:"protocolPath"`
	ReportURL         string       `json:"reportUrl"`
	WallClockMs       *float64     `json:"wallClockMs"`
	TotalInputTokens  *float64     `json:"totalInputTokens"`
	TotalOutputTokens *float64     `json:"totalOutputTokens"`
	Items             []bazaarItem `json:"items"`
	Warnings          []struct {
		ID       string `json:"id"`
		Label    string `json:"label"`
		Severity string `json:"severity"`
	} `json:"warnings"`
	IdentityAssessment *struct {
		Status          string `json:"status"`
		ClaimedModel    string `json:"claimedModel"`
		PredictedFamily string `json:"predictedFamily"`
		Confidence      any    `json:"confidence"`
		RiskFlags       []any  `json:"riskFlags"`
		Verdict         *struct {
			Status      string `json:"status"`
			TrueFamily  string `json:"trueFamily"`
			TrueModel   string `json:"trueModel"`
			SpoofMethod string `json:"spoofMethod"`
			Confidence  any    `json:"confidence"`
			Reasoning   any    `json:"reasoning"`
		} `json:"verdict"`
		Warnings []struct {
			ID       string `json:"id"`
			Label    string `json:"label"`
			Severity string `json:"severity"`
		} `json:"warnings"`
	} `json:"identityAssessment"`
}

func (b *bazaarLink) Poll(ctx context.Context, id string) (*Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.base+"/api/probe/run/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, err
	}
	resp, err := b.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bazaarlink poll: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, ErrRunNotFound
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, &RateLimitedError{RetryAfter: retryAfter(resp.Header, data)}
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("bazaarlink poll: http %d: %s", resp.StatusCode, errorText(data))
	}
	var run bazaarRun
	if err := json.Unmarshal(data, &run); err != nil {
		return nil, fmt.Errorf("bazaarlink poll: decode: %w", err)
	}
	return run.result(b.base), nil
}

func (run bazaarRun) result(base string) *Result {
	switch run.Status {
	case "completed":
	case "failed", "aborted":
		return &Result{Status: "failed", Outcome: OutcomeInconclusive, Summary: run.summary(), Error: "detection run " + run.Status}
	default:
		return &Result{Status: "running"}
	}
	res := &Result{Status: "completed", Outcome: run.outcome(), Summary: run.summary()}
	if run.ReportURL != "" {
		if strings.HasPrefix(run.ReportURL, "http") {
			res.ReportURL = run.ReportURL
		} else {
			res.ReportURL = base + run.ReportURL
		}
	}
	return res
}

// outcome 以详细判定（verdict.status）为准，没有时退回外层分类（status）。
func (run bazaarRun) outcome() string {
	ia := run.IdentityAssessment
	if ia == nil {
		return OutcomeInconclusive // 被测端点请求全部失败时没有身份评估
	}
	if ia.Verdict != nil && ia.Verdict.Status != "" {
		switch ia.Verdict.Status {
		case "clean_match", "clean_match_family_only":
			return OutcomePass
		case "clean_match_submodel_mismatch":
			return OutcomeSubmodelMismatch
		case "plain_mismatch":
			return OutcomeMismatch
		case "spoof_behavior_induced", "spoof_selfclaim_forged":
			return OutcomeSpoof
		default: // ambiguous、insufficient_data
			return OutcomeInconclusive
		}
	}
	switch ia.Status {
	case "match":
		return OutcomePass
	case "mismatch":
		return OutcomeMismatch
	}
	return OutcomeInconclusive
}

// categoryNames 是检测站分组到中文大类名的映射；未登记的分组保留原始 key 作 name，不丢项。
var categoryNames = map[string]string{
	"identity":  "身份识别",
	"submodel":  "子模型识别",
	"quality":   "品质测试",
	"safety":    "安全探针",
	"integrity": "完整性检查",
}

const (
	// riskReasonLimit 是单条风险原因的字符上限，避免把检测站的原始长文本整段透出。
	riskReasonLimit = 160
	// riskLimit 是风险项条数上限。
	riskLimit = 20
)

func categoryName(key string) string {
	if name, ok := categoryNames[key]; ok {
		return name
	}
	return key
}

// truncateRunes 按字符截断并去掉 text 列不接受的无效 UTF-8 与 NUL。
func truncateRunes(s string, n int) string {
	s = strings.ReplaceAll(strings.ToValidUTF8(s, ""), "\x00", "")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

type bazaarItem struct {
	ProbeID    string `json:"probeId"`
	Label      string `json:"label"`
	Group      string `json:"group"`
	Status     string `json:"status"`
	Passed     any    `json:"passed"` // true / false / "warning" / null
	PassReason string `json:"passReason"`
	Error      string `json:"error"`
}

// categoryCount 是一大类的逐项统计，passed/warning/failed/error 之外的项（pending、skipped 等）
// 只计入 total，避免把未完成的项算成通过。
type categoryCount struct {
	Key     string `json:"key"`
	Name    string `json:"name"`
	Total   int    `json:"total"`
	Passed  int    `json:"passed"`
	Warning int    `json:"warning"`
	Failed  int    `json:"failed"`
	Error   int    `json:"error"`
}

// risk 是卖家可见的风险项：只收 failed 与 warning，error 属于我们侧或网络问题，不作为卖家风险展示。
type risk struct {
	Category     string `json:"category"`
	CategoryName string `json:"category_name"`
	Label        string `json:"label"`
	Severity     string `json:"severity"` // fail / warn
	Reason       string `json:"reason"`
}

func passReasonOf(it bazaarItem) string {
	if reason := strings.TrimSpace(it.PassReason); reason != "" {
		return truncateRunes(reason, riskReasonLimit)
	}
	if detail := strings.TrimSpace(it.Error); detail != "" {
		return truncateRunes(detail, riskReasonLimit)
	}
	return ""
}

// categoriesAndRisks 按分组汇总逐项明细，并按分组首次出现的顺序输出。
func categoriesAndRisks(items []bazaarItem) ([]categoryCount, []risk) {
	order := []string{}
	counts := map[string]*categoryCount{}
	var failed, warnings []risk
	for _, it := range items {
		key := it.Group
		count, ok := counts[key]
		if !ok {
			count = &categoryCount{Key: key, Name: categoryName(key)}
			counts[key] = count
			order = append(order, key)
		}
		count.Total++
		switch {
		case it.Status == "error" || it.Status == "failed" || strings.TrimSpace(it.Error) != "":
			count.Error++
			continue
		}
		switch passed := it.Passed.(type) {
		case bool:
			if passed {
				count.Passed++
				continue
			}
			count.Failed++
			if len(failed) < riskLimit {
				failed = append(failed, risk{Category: key, CategoryName: count.Name, Label: it.Label, Severity: "fail", Reason: passReasonOf(it)})
			}
		case string:
			if passed != "warning" {
				// 其他字符串取值（pending、skipped 等）只计入 total。
				continue
			}
			count.Warning++
			if len(warnings) < riskLimit {
				warnings = append(warnings, risk{Category: key, CategoryName: count.Name, Label: it.Label, Severity: "warning", Reason: passReasonOf(it)})
			}
		}
	}
	out := make([]categoryCount, 0, len(order))
	for _, key := range order {
		out = append(out, *counts[key])
	}
	// 先失败后警告，再按上限截断，保证 warning 不会把 failed 挤出风险列表。
	risks := append(failed, warnings...)
	if len(risks) > riskLimit {
		risks = risks[:riskLimit]
	}
	return out, risks
}

func (run bazaarRun) summary() map[string]any {
	s := map[string]any{"run_status": run.Status, "model": run.ModelID}
	if run.ProtocolPath != "" {
		s["protocol_path"] = run.ProtocolPath
	}
	items := map[string]int{"total": len(run.Items)}
	var failed []map[string]string
	for _, it := range run.Items {
		items[it.Status]++
		switch v := it.Passed.(type) {
		case bool:
			if v {
				items["passed"]++
			} else {
				items["failed"]++
				if len(failed) < 10 {
					failed = append(failed, map[string]string{"label": it.Label, "group": it.Group, "reason": truncate(it.PassReason, 160)})
				}
			}
		case string:
			items[v]++
		}
	}
	s["items"] = items
	if categories, risks := categoriesAndRisks(run.Items); len(categories) > 0 {
		s["categories"] = categories
		s["risks"] = risks
	}
	if len(failed) > 0 {
		s["failed_probes"] = failed
	}
	if run.TotalInputTokens != nil || run.TotalOutputTokens != nil {
		s["tokens"] = map[string]any{"input": run.TotalInputTokens, "output": run.TotalOutputTokens}
	}
	if run.WallClockMs != nil {
		s["wall_clock_ms"] = *run.WallClockMs
	}
	var warnings []map[string]string
	add := func(id, label, severity string) {
		warnings = append(warnings, map[string]string{"id": id, "label": label, "severity": severity})
	}
	for _, w := range run.Warnings {
		add(w.ID, w.Label, w.Severity)
	}
	if ia := run.IdentityAssessment; ia != nil {
		s["identity_status"] = ia.Status
		if ia.PredictedFamily != "" {
			s["predicted_family"] = ia.PredictedFamily
		}
		if ia.Confidence != nil {
			s["confidence"] = ia.Confidence
		}
		var flags []string
		for _, f := range ia.RiskFlags {
			if str, ok := f.(string); ok {
				flags = append(flags, str)
			}
		}
		if len(flags) > 0 {
			s["risk_flags"] = flags
		}
		if v := ia.Verdict; v != nil {
			s["verdict_status"] = v.Status
			for k, val := range map[string]string{"true_family": v.TrueFamily, "true_model": v.TrueModel, "spoof_method": v.SpoofMethod} {
				if val != "" {
					s[k] = val
				}
			}
			if v.Confidence != nil {
				s["confidence"] = v.Confidence
			}
			if r, ok := v.Reasoning.(string); ok && r != "" {
				s["reasoning"] = truncate(r, 600)
			}
		}
		if len(run.Warnings) == 0 {
			for _, w := range ia.Warnings {
				add(w.ID, w.Label, w.Severity)
			}
		}
	}
	if len(warnings) > 0 {
		s["warnings"] = warnings
	}
	return s
}
