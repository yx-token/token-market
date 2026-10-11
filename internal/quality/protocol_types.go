package quality

// ProbeMessage 使用协议无关内容块保存真实会话历史。
type ProbeMessage struct {
	Role    string         `json:"role"`
	Content []ProbeContent `json:"content"`
}

type ProbeContent struct {
	Type         string         `json:"type"`
	Text         string         `json:"text,omitempty"`
	AttachmentID string         `json:"attachment_id,omitempty"`
	ID           string         `json:"id,omitempty"`
	Name         string         `json:"name,omitempty"`
	Input        map[string]any `json:"input,omitempty"`
	ToolUseID    string         `json:"tool_use_id,omitempty"`
	Content      any            `json:"content,omitempty"`
}

type ProbeTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema"`
}

type ProbeAttachment struct {
	ID        string `json:"id"`
	MediaType string `json:"media_type"`
	Encoding  string `json:"encoding"`
	Data      string `json:"data"`
}

type ProbeExpected struct {
	Outcome    string   `json:"outcome,omitempty"`
	HTTPStatus []int    `json:"http_status,omitempty"`
	ErrorCodes []string `json:"error_codes,omitempty"`
}

// ProbeStep 仅描述本地确定性增量，不执行模型返回的代码或外部工具。
type ProbeStep struct {
	Expected    *Grader        `json:"expected,omitempty"`
	Request     *ProbeRequest  `json:"request,omitempty"`
	UsePrevious bool           `json:"use_previous,omitempty"`
	ToolResults map[string]any `json:"tool_results,omitempty"`
}

type ProbeToolCall struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type ToolCall = ProbeToolCall

func (r ProbeRequest) effectiveEffort() string {
	if r.Effort != "" {
		return r.Effort
	}
	return r.ReasoningEffort
}

func (r ProbeResult) HasFlag(flag string) bool {
	for _, f := range r.Flags {
		if f == flag {
			return true
		}
	}
	return false
}

// EligibleForIntelligence 排除不能归因于答题能力的观测。
func (r ProbeResult) EligibleForIntelligence() bool {
	return r.OK() && !r.HasFlag("refusal") && !r.HasFlag("truncated") && !r.HasFlag("stop_error")
}

func (r *ProbeResult) addFlag(flag string) {
	if !r.HasFlag(flag) {
		r.Flags = append(r.Flags, flag)
	}
}
