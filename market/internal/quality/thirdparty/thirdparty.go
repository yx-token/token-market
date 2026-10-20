// Package thirdparty 对接第三方模型检测站：把被测端点交给检测站，轮询结果并折算成统一的结论。
//
// 每个检测站实现 Provider；新增检测站时实现接口并在 factories 中登记即可，管理后台会自动出现对应配置。
package thirdparty

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"
)

// Target 是交给检测站的被测端点（OpenAI / Anthropic 兼容协议）。
type Target struct {
	BaseURL      string // API 根地址，包含 /v1，例如 https://api.example.com/v1
	APIKey       string
	Model        string
	ClaimedModel string
	Format       string // openai（Chat Completions）/ anthropic（Messages）
}

// 检测结果的归类（与检测站无关）。
const (
	OutcomePass             = "pass"              // 身份与声明一致
	OutcomeSubmodelMismatch = "submodel_mismatch" // 同一家族，但不是声明的具体型号
	OutcomeMismatch         = "mismatch"          // 证据指向其他家族
	OutcomeSpoof            = "spoof"             // 伪装成声明的模型
	OutcomeInconclusive     = "inconclusive"      // 证据不足或检测失败
)

// Result 是一次轮询的结果。Summary 已去掉被测端点的原始回答等敏感内容，可以保存并展示给卖家。
type Result struct {
	Status    string // running / completed / failed
	Outcome   string // 完成时的归类
	Summary   map[string]any
	ReportURL string
	Error     string
}

// RateLimitedError 表示检测站限流，需要等待 RetryAfter 后再提交。
type RateLimitedError struct {
	RetryAfter time.Duration
}

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("rate limited, retry after %s", e.RetryAfter)
}

// ErrRunNotFound 表示检测站已经没有这次检测（例如结果过期）。
var ErrRunNotFound = errors.New("thirdparty: run not found")

type Provider interface {
	Name() string
	Submit(ctx context.Context, t Target) (externalID string, err error)
	Poll(ctx context.Context, externalID string) (*Result, error)
}

// Config 是管理员保存的检测站配置。
type Config struct {
	BaseURL string
	APIKey  string
	Options map[string]any
}

// Factory 描述一个可接入的检测站。
type Factory struct {
	Name           string `json:"name"`
	Label          string `json:"label"`
	Description    string `json:"description"`
	DocsURL        string `json:"docs_url"`
	DefaultBaseURL string `json:"default_base_url"`
	// Options 是该检测站支持的可选参数（键 → 说明），后台按此渲染
	Options map[string]string `json:"options"`
	// RunsPerHourLimit 是检测站公布的提交频率上限（同一出口 IP）
	RunsPerHourLimit int `json:"runs_per_hour_limit"`
	// RequestsPerRun 是每次检测大约向被测端点发出的请求数，用于提示成本
	RequestsPerRun int                                            `json:"requests_per_run"`
	New            func(cfg Config, client *http.Client) Provider `json:"-"`
}

var factories = map[string]Factory{}

func register(f Factory) { factories[f.Name] = f }

// Known 返回全部可接入的检测站。
func Known() []Factory {
	out := make([]Factory, 0, len(factories))
	for _, f := range factories {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func Lookup(name string) (Factory, bool) {
	f, ok := factories[name]
	return f, ok
}

// New 按名称与配置创建检测站客户端。
func New(name string, cfg Config, client *http.Client) (Provider, error) {
	f, ok := factories[name]
	if !ok {
		return nil, fmt.Errorf("unknown detection provider %q", name)
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = f.DefaultBaseURL
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return f.New(cfg, client), nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
