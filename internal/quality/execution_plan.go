package quality

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const maxExecutionSnapshot = 32 << 20
const maxStepState = 32 << 10

type plannedStep struct {
	Answer      string         `json:"answer,omitempty"`
	Needle      string         `json:"needle,omitempty"`
	Step        int            `json:"step"`
	Request     ProbeRequest   `json:"request"`
	Expected    Grader         `json:"expected"`
	UsePrevious bool           `json:"use_previous,omitempty"`
	ToolResults map[string]any `json:"tool_results,omitempty"`
}

type plannedInstance struct {
	Item             Item           `json:"item"`
	ItemID           int64          `json:"item_id"`
	InstanceIdx      int            `json:"instance_idx"`
	CheckKey         string         `json:"check_key"`
	GeneratorVersion string         `json:"generator_version"`
	Hash             string         `json:"instance_hash"`
	TargetOnly       bool           `json:"target_only,omitempty"`
	RequestedModel   string         `json:"requested_model"`
	Seed             string         `json:"seed"`
	Parameters       map[string]any `json:"parameters"`
	Needle           string         `json:"needle,omitempty"`
	Steps            []plannedStep  `json:"steps"`
}

type executionSnapshot struct {
	Settings              *InspectionSettings `json:"inspection_settings,omitempty"`
	Warnings              []string            `json:"warnings,omitempty"`
	RequestTimeoutSeconds int64               `json:"request_timeout_seconds,omitempty"`
	Policy                *Policy             `json:"policy,omitempty"`
	FlowModes             map[string]string   `json:"flow_modes,omitempty"`
	Flows                 []string            `json:"flows,omitempty"`
	Models                []string            `json:"models,omitempty"`
	ModelWhitelist        []string            `json:"model_whitelist"`
	Limits                struct {
		Account int `json:"account_parallelism"`
		Global  int `json:"global_parallelism"`
	} `json:"limits"`
	SchemaVersion  int               `json:"schema_version"`
	PolicyVersion  int64             `json:"policy_version"`
	InspectionMode string            `json:"inspection_mode"`
	Mode           string            `json:"mode"`
	Budget         TokenBudget       `json:"token_budget"`
	Scoring        bool              `json:"scoring"`
	Thresholds     Thresholds        `json:"thresholds"`
	Instances      []plannedInstance `json:"instances"`
}

// HMAC 输入用 JSON 数组编码并加用途前缀，防止分隔歧义；仅派生值入库。
func (e *Engine) instanceSeed(batch string, itemID int64, idx int, version string) string {
	return e.Keyring.BlindIndex("token-market/inspection/seed/" + string(mustJSON([]any{batch, itemID, idx, version})))
}

func (e *Engine) executionPlan(ctx context.Context, run runRow, p *Policy, items []Item, st InspectionSettings, timeouts ...time.Duration) (executionSnapshot, error) {
	var raw []byte
	err := e.DB.QueryRow(ctx, `SELECT execution_snapshot FROM probe_runs WHERE batch_id = $1 AND trigger <> 'reference' ORDER BY id LIMIT 1`, run.batch).Scan(&raw)
	if err != nil {
		return executionSnapshot{}, err
	}
	var frozen executionSnapshot
	if string(raw) != "{}" {
		if err := decodeInspectionJSON(raw, &frozen); err != nil {
			return frozen, err
		}
		if frozen.SchemaVersion == 1 {
			return frozen, nil
		}
		if frozen.SchemaVersion != 0 {
			return frozen, errors.New("质检执行快照版本不受支持")
		}
		if frozen.Policy != nil {
			p = frozen.Policy
		}
	}
	if len(items) == 0 && run.trigger != "recovery" {
		return executionSnapshot{}, errors.New("该厂商与型号没有可执行的质检题")
	}
	if e.Keyring == nil {
		return executionSnapshot{}, errors.New("未配置质检 seed 密钥")
	}
	mode := run.mode
	if mode == "" {
		mode = "legacy"
	}
	budgetMode := mode
	if mode == "legacy" {
		budgetMode = "full"
	}
	plan := executionSnapshot{Policy: p, SchemaVersion: 1, PolicyVersion: p.Version, InspectionMode: st.Mode, Mode: mode, Budget: st.TokenBudgets[budgetMode], Thresholds: p.Thresholds, FlowModes: frozen.FlowModes, Flows: run.flows, Models: run.models, ModelWhitelist: frozen.ModelWhitelist}
	plan.Settings = &st
	plan.Limits.Account, plan.Limits.Global = st.Limits.Account, st.Limits.Global
	if len(timeouts) > 0 {
		plan.RequestTimeoutSeconds = int64(timeouts[0] / time.Second)
	}
	if b, ok := p.TokenBudgets[budgetMode]; ok {
		plan.Budget = b
	}
	plan.Budget.Input *= int64(max(1, len(plan.Models)))
	plan.Budget.Output *= int64(max(1, len(plan.Models)))
	orderedItems := append([]Item(nil), items...)
	sort.SliceStable(orderedItems, func(i, j int) bool {
		return orderedItems[i].ID < orderedItems[j].ID
	})
	planBytes := len(mustJSON(plan)) + 65536
	appendInstance := func(instance plannedInstance) bool {
		size := len(mustJSON(instance)) + 1
		if len(plan.Instances) >= executionInstanceLimit(plan) || planBytes+size > maxExecutionSnapshot {
			if !containsAny(plan.Warnings, "检测计划超过实例或快照上限，已停止扩样") {
				plan.Warnings = append(plan.Warnings, "检测计划超过实例或快照上限，已停止扩样")
			}
			return false
		}
		plan.Instances = append(plan.Instances, instance)
		planBytes += size
		return true
	}
itemsLoop:
	for _, it := range orderedItems {
		for _, model := range flowItemModels(it, run.model, plan.Models) {
			modelOffset := 0
			if executionItemFlow(it) == "intelligence" {
				for n, candidate := range plan.Models {
					if candidate == model {
						modelOffset = n * 10000
						break
					}
				}
			}
			d, err := parseGenerator(it.Generator)
			if err != nil {
				if strings.HasPrefix(it.Suite, "inspection_") {
					plan.Warnings = append(plan.Warnings, "检测模板配置无效，请管理员检查")
					continue
				}
				return plan, err
			}
			if d != nil {
				count := 1
				if d.Instances != nil {
					count = d.Instances[executionFlowMode(executionItemFlow(it), plan.FlowModes, mode)]
				}
				for idx := 0; idx < count; idx++ {
					if len(plan.Instances) >= executionInstanceLimit(plan) {
						if !containsAny(plan.Warnings, "检测计划超过实例或快照上限，已停止扩样") {
							plan.Warnings = append(plan.Warnings, "检测计划超过实例或快照上限，已停止扩样")
						}
						break
					}
					seed := e.instanceSeed(run.batch, it.ID, modelOffset+idx, d.TemplateID+"/"+d.Version)
					generated, err := generateBankProbe(*d, seed)
					if err != nil {
						plan.Warnings = append(plan.Warnings, "检测实例生成失败，请管理员检查模板参数")
						continue
					}
					req := mergeStepRequest(it.Request, generated.Request)
					req.Model, req.MaxTokens = model, it.MaxTokens
					if req.effectiveEffort() == "" && it.ReasoningEffort != nil {
						req.ReasoningEffort = *it.ReasoningEffort
					}
					instance := plannedInstance{Item: Item{ID: it.ID, Suite: it.Suite, Category: it.Category, Version: it.Version}, ItemID: it.ID, InstanceIdx: modelOffset + idx, RequestedModel: model, CheckKey: it.CheckKey, GeneratorVersion: d.TemplateID + "/" + d.Version, Seed: seed, Parameters: generated.Parameters, Steps: []plannedStep{{Request: req, Expected: generated.Grader, Answer: generated.Answer}}}
					if err := validateInspectionGrader(generated.Grader); err != nil && inspectionGraderTypes[generated.Grader.Type] {
						plan.Warnings = append(plan.Warnings, "检测实例判分配置无效，请管理员检查")
						continue
					}
					sum := sha256.Sum256(mustJSON(instance))
					instance.Hash = hex.EncodeToString(sum[:])
					if !appendInstance(instance) {
						break
					}
				}
				continue
			}
			seed := e.instanceSeed(run.batch, it.ID, modelOffset, "static-1")
			a, _ := strconv.ParseUint(seed[:16], 16, 64)
			b, _ := strconv.ParseUint(seed[16:32], 16, 64)
			rng := rand.New(rand.NewPCG(a, b)) //nolint:gosec // 派生 seed 已由平台 HMAC 保护。
			// 静态请求按模型独立复制，扩展占位符不能改写下一模型的题面。
			var req ProbeRequest
			if err := decodeInspectionJSON(mustJSON(it.Request), &req); err != nil {
				return plan, err
			}
			req.Model, req.MaxTokens = model, it.MaxTokens
			if req.Prompt == "" && len(req.Messages) == 0 {
				req.Prompt = it.Prompt
			}
			var needle string
			req.Prompt, needle = expandFrozen(req.Prompt, rng)
			for mi := range req.Messages {
				for ci := range req.Messages[mi].Content {
					text := req.Messages[mi].Content[ci].Text
					if text != "" {
						expanded, n := expandFrozen(text, rng)
						req.Messages[mi].Content[ci].Text = expanded
						if n != "" {
							needle = n
						}
					}
				}
			}
			if req.effectiveEffort() == "" && it.ReasoningEffort != nil {
				req.ReasoningEffort = *it.ReasoningEffort
			}
			steps := req.Steps
			req.Steps = nil
			instance := plannedInstance{Item: Item{ID: it.ID, Suite: it.Suite, Category: it.Category, Version: it.Version}, ItemID: it.ID, InstanceIdx: modelOffset, RequestedModel: model, CheckKey: inspectionItemKey(it), GeneratorVersion: "static-1", Seed: seed, Needle: needle, Parameters: map[string]any{}, Steps: []plannedStep{{Request: req, Expected: it.Grader, Answer: staticAnswer(it.Grader)}}}
			for i, step := range steps {
				next := req
				if step.Request != nil {
					next = mergeStepRequest(req, *step.Request)
				}
				next.Steps = nil
				var stepNeedle string
				next.Prompt, stepNeedle = expandFrozen(next.Prompt, rng)
				for mi := range next.Messages {
					for ci := range next.Messages[mi].Content {
						text := next.Messages[mi].Content[ci].Text
						expanded, n := expandFrozen(text, rng)
						next.Messages[mi].Content[ci].Text = expanded
						if n != "" {
							stepNeedle = n
						}
					}
				}
				g := it.Grader
				if step.Expected != nil {
					g = *step.Expected
				}
				if _, err := grade(g, "", ""); err != nil {
					if strings.HasPrefix(it.Suite, "inspection_") {
						plan.Warnings = append(plan.Warnings, "检测步骤配置无效，请管理员检查")
						continue itemsLoop
					}
					return plan, err
				}
				instance.Steps = append(instance.Steps, plannedStep{Step: i + 1, Request: next, Expected: g, Answer: staticAnswer(g), Needle: stepNeedle, UsePrevious: step.UsePrevious, ToolResults: step.ToolResults})
			}

			// 哈希包含题面、判分器、参数与完整静态计划，排除账号凭据。
			sum := sha256.Sum256(mustJSON(instance))
			instance.Hash = hex.EncodeToString(sum[:])
			appendInstance(instance)
		}
	}
	addInspectionProbes(&plan, p, st)
	for _, warning := range plan.Warnings {
		if warning == "检测计划超过实例或快照上限，已停止扩样" || warning == "检测计划超过实例上限，已停止扩样" {
			return plan, errors.New("检测计划超出实例或快照上限，请减少题目扩样；本次不能保证全部模型覆盖")
		}
	}
	if mode == "full" && run.trigger == "recovery" {
		var failed []string
		err := e.DB.QueryRow(ctx, `SELECT last_failed_checks FROM upstream_quality_state WHERE upstream_id=$1`, run.upstreamID).Scan(&failed)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return plan, err
		}
		if containsAny(failed, "D13", "D04") {
			carrier, err := loadAvailabilityRecoveryProbe(ctx, e.DB, run.model)
			if err != nil {
				return plan, err
			}
			if err := e.addAvailabilityRecoveryProbes(&plan, run.batch, st.Recovery.AvailabilitySamples, carrier); err != nil {
				return plan, err
			}
		}
	}
	sort.SliceStable(plan.Instances, func(i, j int) bool {
		if plan.Instances[i].ItemID != plan.Instances[j].ItemID {
			return plan.Instances[i].ItemID < plan.Instances[j].ItemID
		}
		return plan.Instances[i].InstanceIdx < plan.Instances[j].InstanceIdx
	})
	encoded, err := json.Marshal(plan)
	if err != nil {
		return plan, err
	}
	if len(encoded) > maxExecutionSnapshot {
		return plan, fmt.Errorf("执行快照超过 %d MiB", maxExecutionSnapshot>>20)
	}
	_, err = e.DB.Exec(ctx, `UPDATE probe_runs SET execution_snapshot = $2, planned = $3 WHERE id = $1 AND COALESCE((execution_snapshot->>'schema_version')::int,0)=0`, run.id, encoded, plannedSteps(plan))
	return plan, err
}

// plannedSteps 是快照计划的总题数：所有实例的步骤数之和，与 probe_results 每步一行的口径一致；
// 没有实例（例如纯第三方核验）时为 NULL，界面显示「—」而不是「共 0 题」。
func plannedSteps(plan executionSnapshot) *int {
	if len(plan.Instances) == 0 {
		return nil
	}
	total := 0
	for _, instance := range plan.Instances {
		total += len(instance.Steps)
	}
	return &total
}

func expandFrozen(prompt string, rng *rand.Rand) (string, string) { return expandPrompt(prompt, rng) }

// 多步字段是增量；未覆盖的工具定义、附件、系统指令与输出预算继续沿用。
func mergeStepRequest(base, patch ProbeRequest) ProbeRequest {
	req := base
	if patch.Prompt != "" {
		req.Prompt = patch.Prompt
		req.Messages = nil
	}
	if patch.Messages != nil {
		req.Messages = patch.Messages
		req.Prompt = ""
	}
	if patch.System != "" {
		req.System = patch.System
	}
	if patch.MaxTokens > 0 {
		req.MaxTokens = patch.MaxTokens
	}
	if patch.Tools != nil {
		req.Tools = patch.Tools
	}
	if patch.Attachments != nil {
		req.Attachments = patch.Attachments
	}
	if patch.effectiveEffort() != "" {
		req.Effort = patch.effectiveEffort()
		req.ReasoningEffort = ""
	}
	if patch.Expected.Outcome != "" {
		req.Expected = patch.Expected
	}
	if patch.Extra != nil {
		req.Extra = map[string]any{}
		for k, v := range base.Extra {
			req.Extra[k] = v
		}
		for k, v := range patch.Extra {
			req.Extra[k] = v
		}
	}
	// 深拷贝内容块，冻结后续草堆时不能改动已生成的首步。
	if req.Messages != nil {
		var messages []ProbeMessage
		_ = decodeInspectionJSON(mustJSON(req.Messages), &messages)
		req.Messages = messages
	}
	req.Steps = nil
	return req
}

func decodeInspectionJSON(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	return decoder.Decode(target)
}
