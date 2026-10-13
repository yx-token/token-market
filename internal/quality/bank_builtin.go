package quality

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// 内置题使用检测项和流程选题，旧智力套件不再参与运行。
func inspectionBuiltinItems() []Item {
	high := "high"
	var items []Item
	addGenerator := func(key, name, suite, check, template string, p map[string]any, full, quick int) {
		version := "1"
		if template == "zebra" || template == "constraints" {
			version = "2"
		}
		modes := []string{"full"}
		if quick > 0 {
			modes = append(modes, "quick")
		}
		items = append(items, Item{BuiltinKey: &key, Suite: suite, CheckKey: check, Category: name, Prompt: name + "（现场参数化生成，仅输出最终答案）", Grader: Grader{Type: "exact", Answer: "由生成器冻结"}, Generator: mustJSON(generatorDefinition{TemplateID: template, Version: version, Parameters: p, Instances: map[string]int{"full": full, "quick": quick}}), Modes: modes, MaxTokens: 8192, ReasoningEffort: &high, Status: "active", Weight: 1})
	}
	for i, row := range []struct{ key, name string }{{"zebra", "斑马谜题"}, {"knights_knaves", "骑士与无赖"}, {"register_machine", "寄存器机"}, {"aime_math", "AIME 式数学"}, {"code_output", "代码输出预测"}, {"string_date", "字符串与日期"}} {
		quick := 3
		if i < 2 {
			quick = 4
		}
		var parameters map[string]any
		version := "1"
		if row.key == "zebra" {
			// 前沿模型起步使用 5×5，实际区分度待运营校准。
			parameters = map[string]any{"houses": 5, "attributes": 5}
			version = "2"
		}
		addGenerator("inspection/"+row.key+"/"+version, row.name, "inspection_capability", "D07", row.key, parameters, 8, quick)
	}
	for _, lang := range []string{"zh", "en"} {
		addGenerator("inspection/constraints/"+lang+"/2", "可验证约束（"+lang+"）", "inspection_capability", "D07", "constraints", map[string]any{"lang": lang}, 3, 1)
	}
	variants := []string{"multi_needle", "multi_hop", "aggregate", "edges", "associative"}
	for i := 0; i < 10; i++ {
		length := 8192
		if i >= 3 {
			length = 32768
		}
		if i >= 6 {
			length = 131072
		}
		key := fmt.Sprintf("inspection/context/%d/1", i)
		addGenerator(key, "长上下文（"+variants[i%5]+"）", "inspection_context", "D09", "long_context", map[string]any{"target_tokens": length, "variant": variants[i%5]}, 1, 0)
		items[len(items)-1].MaxTokens = 1024
	}
	static := func(key, name, suite, check, prompt string, g Grader, req ProbeRequest, modes []string) {
		items = append(items, Item{BuiltinKey: &key, Suite: suite, CheckKey: check, Category: name, Prompt: prompt, Request: req, Grader: g, Generator: []byte(`{}`), Modes: modes, MaxTokens: 1024, Status: "active", Weight: 1})
	}
	tool := ProbeTool{Name: "lookup_schedule", Description: "只读本地虚拟班次，无网络访问", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"city": map[string]any{"type": "string"}, "date": map[string]any{"type": "string"}}, "required": []string{"city", "date"}, "additionalProperties": false}}
	toolGrader := Grader{Type: "tool_calls", GraderOptions: GraderOptions{Expect: []ExpectedToolCall{{Name: tool.Name, Args: map[string]any{"city": "杭州", "date": "2026-10-12"}}}, Match: "sequence"}}
	finalTool := Grader{Type: "exact", Answer: "09:30"}
	static("inspection/tools/1", "虚拟工具语义", "inspection_function", "A11.tools", "请使用本地班次工具查询杭州 2026-10-12 的班次。不要编造时间。", toolGrader, ProbeRequest{Tools: []ProbeTool{tool}, Steps: []ProbeStep{{UsePrevious: true, Request: &ProbeRequest{Prompt: "只输出工具返回的发车时间。"}, Expected: &finalTool, ToolResults: map[string]any{tool.Name: map[string]any{"departure": "09:30"}}}}}, []string{"full", "quick"})
	static("inspection/schema/1", "结构化输出", "inspection_function", "A11.json_schema", "只输出 JSON 对象：city 为杭州，count 为整数 3，不加其他键或解释。", Grader{Type: "json_schema", Answer: `{"city":"杭州","count":3}`, GraderOptions: GraderOptions{Schema: mustJSON(map[string]any{"type": "object", "properties": map[string]any{"city": map[string]any{"const": "杭州"}, "count": map[string]any{"const": 3, "type": "integer"}}, "required": []string{"city", "count"}, "additionalProperties": false}), NoFence: true}}, ProbeRequest{}, []string{"full", "quick"})
	second := Grader{Type: "exact", Answer: "【蓝色】"}
	static("inspection/multiturn/1", "多轮约束保持", "inspection_function", "A11.multiturn", "这轮和下一轮的回复都只用【】包住一个颜色词。现在输出红色。", Grader{Type: "exact", Answer: "【红色】"}, ProbeRequest{Steps: []ProbeStep{{UsePrevious: true, Request: &ProbeRequest{Prompt: "下一轮颜色是蓝色。"}, Expected: &second}}}, []string{"full"})
	var lines []string
	for i := 1; i <= 60; i++ {
		lines = append(lines, fmt.Sprintf("第%02d项：%d", i, i*i))
	}
	output := strings.Join(lines, "\n")
	digest := sha256.Sum256([]byte(output))
	static("inspection/output/2", "完整枚举输出", "inspection_output", "D08", "从1到60逐行输出平方数，格式第01项：1、第02项：4，编号两位补零。不得省略任何行，不加解释、代码围栏或空行。", Grader{Type: "full_output", Answer: output, GraderOptions: GraderOptions{SHA256: hex.EncodeToString(digest[:]), MinLines: 60, Lazy: "list", RequireNormalStop: true}}, ProbeRequest{}, []string{"full"})
	// Codex 会删除输出上限，完整输出项不适用于该通道。
	items[len(items)-1].Platforms = []string{"anthropic", "gemini", "antigravity", "grok", "kimi", "zhipu", "minimax", "opencode_go"}
	// gpt-6-astra 的测智题。最不利原则：形状可凭手感挑选，取 12 颗五角星 + 9 颗圆形即可，答案 21；当作随机摸取会得出 29。
	// 不要求只回答数字，回答中出现独立的 21 即通过；结果仅代表这道题的表现，不代表模型整体能力。
	// full/quick 各一题，仍受既有硬预算和截止时间限制。
	static("inspection/pigeonhole/astra/1", "pigeonhole", "inspection_capability", "D07", "在一个黑色的袋子里放有三种口味的糖果，每种糖果有两种不同的形状（圆形和五角星形，不同的形状靠手感可以分辨）。现已知不同口味的糖和不同形状的数量统计如下表。参赛者需要在活动前决定摸出的糖果数目，那么，最少取出多少个糖果才能保证手中同时拥有不同形状的苹果味和桃子味的糖？（同时手中有圆形苹果味匹配五角星桃子味糖果，或者有圆形桃子味匹配五角星苹果味糖果都满足要求）\n\n| | 苹果味 | 桃子味 | 西瓜味 |\n|---|---|---|---|\n| 圆形 | 7 | 9 | 8 |\n| 五角星 | 7 | 6 | 4 |", Grader{Type: "regex", Pattern: `(^|[^0-9.])21([^0-9.]|\.([^0-9]|$)|$)`}, ProbeRequest{}, []string{"full", "quick"})
	items[len(items)-1].Models = []string{"gpt-6-astra"}
	items[len(items)-1].ReasoningEffort = &high
	items[len(items)-1].MaxTokens = 8192
	for i, row := range []struct{ amount, answer string }{{"1409.50", "壹仟肆佰零玖元伍角"}, {"16.09", "壹拾陆元零玖分"}} {
		aliases := []string{row.answer}
		if i == 0 {
			aliases = append(aliases, row.answer+"整", row.answer+"正")
		}
		static(fmt.Sprintf("inspection/amount/%d/1", i), "中文大写金额", "inspection_capability", "D07", "将人民币"+row.amount+"元写为中文大写金额，仅输出金额文字。", Grader{Type: "lines_alias", GraderOptions: GraderOptions{Lines: [][]string{aliases}}}, ProbeRequest{}, []string{"full"})
	}
	static("inspection/abstain/2", "缺失信息弃权", "inspection_capability", "D07.honesty", "资料仅写：一盒彩球。资料没有记载数量。问盒内共有多少球？无法由资料确定时只输出“无法确定”。", Grader{Type: "abstain", Answer: "无法确定", GraderOptions: GraderOptions{Patterns: []string{`^无法确定$`}, NegativePatterns: []string{`[0-9]`}}}, ProbeRequest{}, []string{"full"})
	return append(items, inspectionAdditionalItems()...)
}

// SyncInspectionItems 按稳定键只补缺项；管理员编辑、下线与版本均原样保留。
func SyncInspectionItems(ctx context.Context, db DB) (int, error) {
	created := 0
	for _, it := range inspectionBuiltinItems() {
		if err := validateItem(it); err != nil {
			return created, err
		}
		platforms, models := it.Platforms, it.Models
		if platforms == nil {
			platforms = []string{}
		}
		if models == nil {
			models = []string{}
		}
		tag, err := db.Exec(ctx, `INSERT INTO probe_items(suite,category,prompt,grader,platforms,models,max_tokens,reasoning_effort,status,check_key,request,generator,modes,builtin_key,weight) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) ON CONFLICT(builtin_key) DO NOTHING`, it.Suite, it.Category, it.Prompt, mustJSON(it.Grader), platforms, models, it.MaxTokens, it.ReasoningEffort, it.Status, it.CheckKey, mustJSON(it.Request), it.Generator, it.Modes, it.BuiltinKey, it.Weight)
		if err != nil {
			return created, err
		}
		created += int(tag.RowsAffected())
	}
	return created, nil
}
