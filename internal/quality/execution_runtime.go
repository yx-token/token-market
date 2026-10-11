package quality

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DueGin/token-market/internal/sub2api/replica"
	"github.com/jackc/pgx/v5"
)

type resumedState struct {
	RetryAfterMS  int64           `json:"retry_after_ms,omitempty"`
	Complete      bool            `json:"complete"`
	Text          string          `json:"text,omitempty"`
	ToolCalls     []ProbeToolCall `json:"tool_calls,omitempty"`
	InputCharged  int64           `json:"input_charged,omitempty"`
	OutputCharged int64           `json:"output_charged,omitempty"`
}

type sideBudget struct {
	sync.Mutex
	used  TokenBudget
	limit TokenBudget
	soft  bool // 旧题只告警，不能引入新的检测失败方式。
}

func (b *sideBudget) reserve(input, output int64) bool {
	b.Lock()
	defer b.Unlock()
	if !b.soft && (input > b.limit.Input-b.used.Input || output > b.limit.Output-b.used.Output) {
		return false
	}
	b.used.Input += input
	b.used.Output += output
	return true
}

func (e *Engine) accountSnapshot(ctx context.Context, u *upstreamRow) (*replica.Account, error) {
	if e.Replica == nil || u == nil || u.accountID == nil {
		return nil, nil
	}
	accounts, err := e.Replica.TMAccounts(ctx)
	if err != nil {
		return nil, err
	}
	for i := range accounts {
		if accounts[i].ID == *u.accountID {
			return &accounts[i], nil
		}
	}
	return nil, nil
}

// 只消费现有被动快照；过期窗口不阻塞，缺失数据明确标为未知。
func windowStatus(a *replica.Account, now time.Time) (known, insufficient bool) {
	if a == nil {
		return false, false
	}
	for _, k := range []string{"session_window_utilization", "passive_usage_7d_utilization", "passive_usage_7d_oi_utilization", "codex_5h_used_percent", "codex_7d_used_percent"} {
		v, ok := a.Extra[k].(float64)
		if !ok || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			continue
		}
		scale := 1.0
		resetKey := "session_window_reset"
		if strings.Contains(k, "7d") {
			resetKey = "passive_usage_7d_reset"
		}
		if strings.Contains(k, "_oi_") {
			resetKey = "passive_usage_7d_oi_reset"
		}
		if strings.HasPrefix(k, "codex_") {
			scale = 100
			resetKey = strings.TrimSuffix(k, "used_percent") + "reset_at"
		}
		if k == "session_window_utilization" {
			if a.SessionWindowEnd == nil || a.SessionWindowEnd.Before(now) {
				continue
			}
			known = true
			if v >= scale {
				insufficient = true
			}
			continue
		}
		raw, present := a.Extra[resetKey]
		if !present {
			continue
		}
		reset, valid := quotaReset(raw)
		if !valid || reset.Before(now) {
			continue
		}
		known = true
		if v >= scale {
			insufficient = true
		}
	}
	return known, insufficient
}

func quotaReset(v any) (time.Time, bool) {
	switch x := v.(type) {
	case float64:
		if x > 0 {
			return time.Unix(int64(x), 0), true
		}
	case int64:
		if x > 0 {
			return time.Unix(x, 0), true
		}
	case string:
		if ts, err := time.Parse(time.RFC3339, x); err == nil {
			return ts, true
		}
		if n, err := strconv.ParseInt(x, 10, 64); err == nil && n > 0 {
			return time.Unix(n, 0), true
		}
	}
	return time.Time{}, false
}

// 全局与账号计数跨批次共享；每次派发都读取当前下发并发，不能绕过 suspected 的减半。
func (e *Engine) acquireRequest(ctx context.Context, u *upstreamRow, p *Policy, st InspectionSettings) (func(), error) {
	for {
		a, err := e.accountSnapshot(ctx, u)
		if err != nil {
			return nil, err
		}
		actual := 1
		if a != nil {
			actual = max(1, a.Concurrency)
		}
		upper := p.ItemParallelism
		if upper < 1 {
			upper = 4
		}
		limit := min(upper, st.Limits.Account, actual)
		key := u.id
		if u.accountID != nil {
			key = *u.accountID
		}
		e.mu.Lock()
		if e.requestRunning == nil {
			e.requestRunning = map[int64]int{}
		}
		if e.requestWake == nil {
			e.requestWake = make(chan struct{})
		}
		if e.requestTotal < st.Limits.Global && e.requestRunning[key] < limit {
			e.requestTotal++
			e.requestRunning[key]++
			e.mu.Unlock()
			return func() {
				e.mu.Lock()
				e.requestTotal--
				e.requestRunning[key]--
				close(e.requestWake)
				e.requestWake = make(chan struct{})
				e.mu.Unlock()
			}, nil
		}
		wake := e.requestWake
		e.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-wake:
		case <-time.After(time.Second):
		}
	}
}

func probeTokenReservation(platform string, req ProbeRequest) (int64, int64, error) {
	if err := validateProbeRequest(platform, req); err != nil {
		return 0, 0, err
	}
	_, body, _ := protocolForModel(platform, req.Model).build(req)
	raw, err := json.Marshal(body)
	if err != nil {
		return 0, 0, err
	}
	input := int64(len(raw)) + 1024 // 校准前按字节保守预留，收到可靠 usage 后修正实际消耗。
	m, err := decodeObject(raw)
	if err != nil {
		return 0, 0, err
	}
	output := int64(req.MaxTokens)
	for _, key := range []string{"max_tokens", "max_output_tokens"} {
		if n := count(m, key); n != nil {
			output = *n
		}
	}
	if _, ok := protocolForModel(platform, req.Model).(geminiProtocol); ok {
		cfg := object(m["generationConfig"])
		if n := count(cfg, "maxOutputTokens"); n != nil {
			output = *n
		}
		thinking := object(cfg["thinkingConfig"])
		if n := count(thinking, "thinkingBudget"); n != nil {
			output += *n
		} else if str(thinking["thinkingLevel"]) != "" {
			output += 65536
		}
	}
	if output <= 0 {
		return 0, 0, errors.New("探测输出上限须为正整数")
	}
	return input, output, nil
}

func (e *Engine) budgetForRun(ctx context.Context, id int64, limit TokenBudget) (*sideBudget, error) {
	var input, output int64
	err := e.DB.QueryRow(ctx, `SELECT COALESCE(sum((step_state->>'input_charged')::bigint),0)::bigint, COALESCE(sum((step_state->>'output_charged')::bigint),0)::bigint FROM probe_results WHERE run_id = $1`, id).Scan(&input, &output)
	if err != nil {
		return nil, err
	}
	var discarded TokenBudget
	var raw []byte
	err = e.DB.QueryRow(ctx, `SELECT COALESCE(summary->'resume_budget','{}') FROM probe_runs WHERE id=$1`, id).Scan(&raw)
	if err == nil {
		err = json.Unmarshal(raw, &discarded)
	}
	return &sideBudget{used: TokenBudget{Input: input + discarded.Input, Output: output + discarded.Output}, limit: limit}, err
}

func (e *Engine) executePlanItems(ctx context.Context, run runRow, ref *runRow, u, refUp *upstreamRow, targetKey, refKey string, plan executionSnapshot, p *Policy, st InspectionSettings, timeout time.Duration) ([]Outcome, []string, error) {
	outcomes := make([]Outcome, len(plan.Instances))
	for i, instance := range plan.Instances {
		outcomes[i] = Outcome{Suite: instance.Item.Suite, ItemID: instance.ItemID, CheckKey: instance.CheckKey}
	}
	if plan.Limits.Account > 0 && plan.Limits.Global > 0 {
		st.Limits.Account, st.Limits.Global = plan.Limits.Account, plan.Limits.Global
	}
	warnings := append([]string(nil), plan.Warnings...)
	windowUnknown := false
	for _, side := range []*upstreamRow{u, refUp} {
		if side == nil {
			continue
		}
		a, err := e.accountSnapshot(ctx, side)
		if err != nil {
			return outcomes, warnings, err
		}
		known, low := windowStatus(a, e.now())
		if low {
			return outcomes, warnings, errWindowInsufficient
		}
		if !known {
			windowUnknown = true
			if !containsAny(warnings, "窗口余量未知") {
				warnings = append(warnings, "窗口余量未知")
			}
		}
	}
	targetBudget, err := e.budgetForRun(ctx, run.id, plan.Budget)
	if err != nil {
		return outcomes, warnings, err
	}
	var refBudget *sideBudget
	if refKey != "" {
		refBudget, err = e.budgetForRun(ctx, ref.id, plan.Budget)
		if err != nil {
			return outcomes, warnings, err
		}
	}
	targetBudget.soft = plan.Mode == "legacy"
	if refBudget != nil {
		refBudget.soft = targetBudget.soft
	}
	errs := make([]error, len(plan.Instances))
	parallel := p.ItemParallelism
	if parallel < 1 {
		parallel = 4
	}
	if plan.Mode == "legacy" {
		parallel = 1
	}
	jobs := make(chan int)
	var workers sync.WaitGroup
	for range min(parallel, len(plan.Instances)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := range jobs {
				instance := plan.Instances[i]
				var tr, rr ProbeResult
				var tp, rp bool
				var te, re error
				var pair sync.WaitGroup
				pair.Add(1)
				go func() {
					defer pair.Done()
					tr, tp, te = e.executeInstance(ctx, run, u, targetKey, instance, p, st, targetBudget, timeout, windowUnknown)
				}()
				if refKey != "" {
					pair.Add(1)
					go func() {
						defer pair.Done()
						rr, rp, re = e.executeInstance(ctx, *ref, refUp, refKey, instance, p, st, refBudget, timeout, windowUnknown)
					}()
				}
				pair.Wait()
				if te != nil {
					outcomes[i] = Outcome{ItemID: instance.ItemID, Suite: instance.Item.Suite, CheckKey: instance.CheckKey, Target: tr}
					errs[i] = te
					continue
				}
				if re != nil {
					outcomes[i] = Outcome{ItemID: instance.ItemID, Suite: instance.Item.Suite, CheckKey: instance.CheckKey, Target: tr, TargetPass: tp, Ref: &rr}
					errs[i] = re
					continue
				}
				o := Outcome{CheckKey: instance.CheckKey, ItemID: instance.ItemID, Suite: instance.Item.Suite, Target: tr, TargetPass: tp}
				if refKey != "" {
					o.Ref, o.RefPass = &rr, rp
				}
				hist, ok, err := e.refHistory(ctx, u.platform, instance.ItemID)
				if err != nil {
					errs[i] = err
					continue
				}
				if ok {
					o.RefHistRate = &hist
				}
				outcomes[i] = o
			}
		}()
	}
	for i := range plan.Instances {
		select {
		case jobs <- i:
		case <-ctx.Done():
			close(jobs)
			workers.Wait()
			return outcomes, warnings, ctx.Err()
		}
	}
	close(jobs)
	workers.Wait()
	for _, err := range errs {
		if err != nil {
			return outcomes, warnings, err
		}
	}
	for _, budget := range []*sideBudget{targetBudget, refBudget} {
		if budget != nil && budget.soft && (budget.used.Input > budget.limit.Input || budget.used.Output > budget.limit.Output) {
			warnings = append(warnings, "旧题模式 token 预算超出，仅告警，检测继续")
			break
		}
	}
	return outcomes, warnings, nil
}

var errWindowInsufficient = errors.New("被动快照明确显示窗口余量不足，检测顺延")

func dynamicRequest(step plannedStep, previous ProbeRequest, state resumedState) ProbeRequest {
	req := step.Request
	if !step.UsePrevious {
		return req
	}
	history := append([]ProbeMessage(nil), previous.Messages...)
	if len(history) == 0 && previous.Prompt != "" {
		history = append(history, ProbeMessage{Role: "user", Content: []ProbeContent{{Type: "text", Text: previous.Prompt}}})
	}
	content := []ProbeContent{}
	if state.Text != "" {
		content = append(content, ProbeContent{Type: "text", Text: state.Text})
	}
	for _, call := range state.ToolCalls {
		content = append(content, ProbeContent{Type: "tool_use", ID: call.ID, Name: call.Name, Input: call.Arguments, LocalID: call.LocalID, ThoughtSignature: call.ThoughtSignature})
	}
	history = append(history, ProbeMessage{Role: "assistant", Content: content})
	if len(state.ToolCalls) > 0 {
		results := []ProbeContent{}
		for _, call := range state.ToolCalls {
			value, ok := step.ToolResults[call.Name]
			if !ok {
				value = map[string]any{"error": "未注册本地工具结果"}
			}
			results = append(results, ProbeContent{Type: "tool_result", ToolUseID: call.ID, Name: call.Name, Content: string(mustJSON(value)), LocalID: call.LocalID})
		}
		history = append(history, ProbeMessage{Role: "user", Content: results})
	}
	if len(req.Messages) > 0 {
		history = append(history, req.Messages...)
	} else if req.Prompt != "" {
		history = append(history, ProbeMessage{Role: "user", Content: []ProbeContent{{Type: "text", Text: req.Prompt}}})
	}
	req.Messages, req.Prompt = history, ""
	return req
}

func (e *Engine) executeInstance(ctx context.Context, run runRow, u *upstreamRow, key string, it plannedInstance, p *Policy, st InspectionSettings, budget *sideBudget, timeout time.Duration, windowUnknown bool) (ProbeResult, bool, error) {
	var forceRerun bool
	if err := e.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM probe_results WHERE run_id=$1 AND item_id=$2 AND instance_idx=$3 AND instance_hash<>'' AND step<$4 AND step_state->>'complete' IS DISTINCT FROM 'true')`, run.id, it.ItemID, it.InstanceIdx, len(it.Steps)-1).Scan(&forceRerun); err != nil {
		return ProbeResult{}, false, err
	}
	previous := ProbeRequest{}
	state := resumedState{}
	pass := true
	var last ProbeResult
	for i, step := range it.Steps {
		if i > 0 && step.UsePrevious && !state.Complete {
			return last, false, errors.New("多步中间状态不完整，须重新执行该题目单元")
		}
		req := dynamicRequest(step, previous, state)
		cached, passed, saved, found, err := e.loadInstanceResult(ctx, run.id, it, i)
		if err != nil {
			return last, false, err
		}
		state = saved
		needsState := i+1 < len(it.Steps) && it.Steps[i+1].UsePrevious
		if found && !forceRerun && (!needsState || state.Complete) {
			last, pass = cached, pass && passed
			if !last.EligibleForIntelligence() && i+1 < len(it.Steps) {
				return last, false, nil
			}
			previous = req
			continue
		}
		input, output, err := probeTokenReservation(u.platform, req)
		if err != nil {
			return last, false, err
		}
		if strings.HasPrefix(it.GeneratorVersion, "long_context/") {
			// 草堆使用模板的词元估算并预留 25% 误差；实际 usage 仍是记账依据。
			var estimate struct {
				Tokens int64 `json:"estimated_input_tokens"`
			}
			if err := decodeInspectionJSON(mustJSON(it.Parameters), &estimate); err != nil || estimate.Tokens <= 0 {
				return last, false, errors.New("长上下文词元估算缺失")
			}
			input = estimate.Tokens*5/4 + 1024
		}
		for attempt := 0; attempt < 3; attempt++ {
			release, err := e.acquireRequest(ctx, u, p, st)
			if err != nil {
				return last, false, err
			}
			if !budget.reserve(input, output) {
				release()
				return last, false, errors.New("质检 token 硬预算不足")
			}
			state.InputCharged += input
			state.OutputCharged += output
			if e.RegisterInstance != nil {
				_, body, _ := protocolForModel(u.platform, req.Model).build(req)
				e.RegisterInstance(ProbeRegistration{CheckKey: it.CheckKey, GeneratorVersion: it.GeneratorVersion, WireBody: mustJSON(body), InstanceHash: it.Hash, Step: i, Platform: u.platform, Request: req, Grader: step.Expected, Answer: step.Answer})
			}
			last = e.probe(ctx, u.platform, key, req, timeout)
			release()
			if windowUnknown {
				last.addFlag("window_unknown")
			}
			actualInput := int64(0)
			if last.InputTokens != nil {
				actualInput = *last.InputTokens
			}
			if u.platform == "anthropic" || strings.Contains(req.Model, "claude") {
				if last.CacheReadTokens != nil {
					actualInput += *last.CacheReadTokens
				}
				if last.CacheCreationTokens != nil {
					actualInput += *last.CacheCreationTokens
				}
			}
			actualOutput := int64(last.OutputTokens)
			if u.platform == "gemini" || (u.platform == "antigravity" && strings.Contains(req.Model, "gemini")) || u.platform == "grok" {
				actualOutput += int64(last.ReasoningTokens)
			}
			extraInput, extraOutput := int64(0), int64(0)
			if last.InputTokens != nil && last.ErrorClass == "" {
				extraInput = actualInput - input
			}
			if last.OutputObserved && last.ErrorClass == "" {
				extraOutput = actualOutput - output
			}
			budget.Lock()
			budget.used.Input += extraInput
			budget.used.Output += extraOutput
			exceeded := budget.used.Input > budget.limit.Input || budget.used.Output > budget.limit.Output
			budget.Unlock()
			state.InputCharged += extraInput
			state.OutputCharged += extraOutput
			state.Text, state.ToolCalls = last.Text, last.ToolCalls
			retry := last.ErrorClass == "rate_limited" || (last.ErrorClass == "platform" && last.HTTPStatus == 429)
			delay, canRetry := inspectionRetryDelay(last.RetryAfter, attempt)
			retry = retry && canRetry
			state.RetryAfterMS = last.RetryAfter.Milliseconds()
			state.Complete = ctx.Err() == nil && (!retry || attempt == 2) && len(mustJSON(state)) <= maxStepState
			if !state.Complete || !needsState {
				state.Text, state.ToolCalls = "", nil
			}
			g := step.Expected
			if g.Type == "accept_reject" && g.Outcome == "" {
				g.Outcome, g.HTTPStatus, g.ErrorCodes = req.Expected.Outcome, req.Expected.HTTPStatus, req.Expected.ErrorCodes
			}
			needle := step.Needle
			if needle == "" {
				needle = it.Needle
			}
			result, err := gradeResult(g, last, needle)
			if err != nil {
				return last, false, err
			}
			for _, f := range result.Flags {
				last.addFlag(f)
			}
			passed = result.Pass && (last.OK() || last.ErrorClass == "expected_reject")
			saveCtx := ctx
			var cancel context.CancelFunc
			if ctx.Err() != nil {
				saveCtx, cancel = context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			}
			err = e.saveInstanceResult(saveCtx, run.id, it, i, last, passed, state)
			if cancel != nil {
				cancel()
			}
			if err != nil {
				return last, false, err
			}
			if ctx.Err() != nil {
				return last, false, ctx.Err()
			}
			if exceeded && !budget.soft {
				return last, false, errors.New("上游返回的实际用量超过质检 token 预算，已停止后续请求")
			}
			if !retry || attempt == 2 {
				break
			}
			timer := time.NewTimer(delay)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return last, false, ctx.Err()
			}
		}
		pass = pass && passed
		if !last.EligibleForIntelligence() && i+1 < len(it.Steps) {
			return last, false, nil
		}
		previous = req
	}
	return last, pass, nil
}

func (e *Engine) loadInstanceResult(ctx context.Context, runID int64, it plannedInstance, step int) (ProbeResult, bool, resumedState, bool, error) {
	var r ProbeResult
	var passed bool
	var raw []byte
	var hash string
	err := e.DB.QueryRow(ctx, `SELECT instance_hash, COALESCE(passed,false), COALESCE(http_status,0), COALESCE(error_class,''), COALESCE(output_tokens,0), COALESCE(reasoning_tokens,0), COALESCE(ttft_ms,0), COALESCE(duration_ms,0), input_tokens, cache_read_tokens, cache_creation_tokens, stop_reason, effort, flags, step_state, reasoning_tokens IS NOT NULL, output_tokens IS NOT NULL FROM probe_results WHERE run_id=$1 AND item_id=$2 AND instance_idx=$3 AND step=$4 AND instance_hash<>''`, runID, it.ItemID, it.InstanceIdx, step).Scan(&hash, &passed, &r.HTTPStatus, &r.ErrorClass, &r.OutputTokens, &r.ReasoningTokens, &r.TTFT, &r.Duration, &r.InputTokens, &r.CacheReadTokens, &r.CacheCreationTokens, &r.StopReason, &r.Effort, &r.Flags, &raw, &r.ReasoningObserved, &r.OutputObserved)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, false, resumedState{}, false, nil
	}
	if err != nil {
		return r, false, resumedState{}, false, err
	}
	if hash != it.Hash {
		return r, false, resumedState{}, false, errors.New("续跑实例哈希不一致")
	}
	var state resumedState
	if err := decodeInspectionJSON(raw, &state); err != nil {
		return r, false, state, false, err
	}
	r.TTFT *= time.Millisecond
	r.Duration *= time.Millisecond
	r.Text, r.ToolCalls = state.Text, state.ToolCalls
	r.RetryAfter = time.Duration(state.RetryAfterMS) * time.Millisecond
	return r, passed, state, state.Complete, nil
}

func (e *Engine) saveInstanceResult(ctx context.Context, runID int64, it plannedInstance, step int, r ProbeResult, passed bool, state resumedState) error {
	if r.Flags == nil {
		r.Flags = []string{}
	}
	_, err := e.DB.Exec(ctx, `INSERT INTO probe_results (run_id,item_id,suite,passed,http_status,error_class,ttft_ms,duration_ms,output_tokens,reasoning_tokens,output_tps,response_model,sub2api_request_id,raw_excerpt,check_key,instance_idx,step,instance_hash,generator_version,input_tokens,cache_read_tokens,cache_creation_tokens,stop_reason,effort,flags,error_body,step_state)
 VALUES ($1,$2,$3,$4,NULLIF($5,0),NULLIF($6,''),$7,$8,$9,$10,$11,NULLIF($12,''),NULLIF($13,''),$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27)
 ON CONFLICT (run_id,item_id,instance_idx,step) WHERE instance_hash<>'' DO UPDATE SET passed=EXCLUDED.passed,http_status=EXCLUDED.http_status,error_class=EXCLUDED.error_class,ttft_ms=EXCLUDED.ttft_ms,duration_ms=EXCLUDED.duration_ms,output_tokens=EXCLUDED.output_tokens,reasoning_tokens=EXCLUDED.reasoning_tokens,output_tps=EXCLUDED.output_tps,response_model=EXCLUDED.response_model,sub2api_request_id=EXCLUDED.sub2api_request_id,raw_excerpt=EXCLUDED.raw_excerpt,input_tokens=EXCLUDED.input_tokens,cache_read_tokens=EXCLUDED.cache_read_tokens,cache_creation_tokens=EXCLUDED.cache_creation_tokens,stop_reason=EXCLUDED.stop_reason,effort=EXCLUDED.effort,flags=EXCLUDED.flags,error_body=EXCLUDED.error_body,step_state=EXCLUDED.step_state`, runID, it.ItemID, it.Item.Suite, passed, r.HTTPStatus, r.ErrorClass, int(r.TTFT/time.Millisecond), int(r.Duration/time.Millisecond), observedOutput(r), observedReasoning(r), r.OutputTPS(), r.ResponseModel, r.Sub2APIRequestID, truncateStr(r.Text, 500), it.CheckKey, it.InstanceIdx, step, it.Hash, it.GeneratorVersion, r.InputTokens, r.CacheReadTokens, r.CacheCreationTokens, r.StopReason, r.Effort, r.Flags, r.ErrorBody, mustJSON(state))
	return err
}

// deferWindow 记录明确不足的原因并留在队列，窗口未知不会走此路径。
func (e *Engine) deferWindow(ctx context.Context, batch string) error {
	_, err := e.DB.Exec(ctx, `UPDATE probe_runs SET status='queued', started_at=NULL, summary=jsonb_build_object('deferred_reason',$2::text) WHERE batch_id=$1 AND status='running'`, batch, errWindowInsufficient.Error())
	return err
}

func observedReasoning(r ProbeResult) *int {
	if r.ReasoningObserved || r.ReasoningTokens > 0 {
		return &r.ReasoningTokens
	}
	return nil
}

func observedOutput(r ProbeResult) *int {
	if r.OutputObserved || r.OutputTokens > 0 {
		return &r.OutputTokens
	}
	return nil
}

// 长窗口限流直接保留证据，不能用一次重试占满整批截止时间。
const maxInspectionRetryWait = 30 * time.Second

func inspectionRetryDelay(retryAfter time.Duration, attempt int) (time.Duration, bool) {
	if retryAfter > maxInspectionRetryWait {
		return 0, false
	}
	if retryAfter > 0 {
		return retryAfter, true
	}
	return time.Duration(1<<attempt) * time.Second, true
}
