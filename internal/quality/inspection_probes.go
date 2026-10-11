package quality

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"

	"github.com/DueGin/token-market/internal/inspection"
)

// 附件由平台自己生成并审阅；无外链、脚本、个人信息或敏感题材。
func inspectionAdditionalItems() []Item {
	makeItem := func(key, check, name, suite, prompt, answer string, req ProbeRequest) Item {
		return Item{BuiltinKey: &key, CheckKey: check, Category: name, Suite: suite, Prompt: prompt, Request: req, Grader: Grader{Type: "exact", Answer: answer}, Generator: []byte(`{}`), Modes: []string{"full"}, MaxTokens: 128, Status: "active", Weight: 1}
	}
	prefix := strings.Repeat("The quiet garden has green trees and smooth stones.\n", 512)
	cachePrompt := prefix + "\n只输出 READY。"
	cacheReq := ProbeRequest{Messages: []ProbeMessage{{Role: "user", Content: []ProbeContent{{Type: "text", Text: cachePrompt, CacheControl: map[string]any{"type": "ephemeral"}}}}}, Steps: []ProbeStep{{Request: &ProbeRequest{Messages: []ProbeMessage{{Role: "user", Content: []ProbeContent{{Type: "text", Text: cachePrompt, CacheControl: map[string]any{"type": "ephemeral"}}}}}}, Expected: &Grader{Type: "exact", Answer: "READY"}}}}
	cache := makeItem("inspection/cache/1", "D12", "缓存两步", "inspection_function", cachePrompt, "READY", cacheReq)
	imageData := inspectionSafeImage()
	img := makeItem("inspection/image/1", "A11.image_pdf", "安全图片识别", "inspection_function", "数出图片中的蓝色方块，只输出数字。", "3", ProbeRequest{Messages: []ProbeMessage{{Role: "user", Content: []ProbeContent{{Type: "text", Text: "数出图片中的蓝色方块，只输出数字。"}, {Type: "image", AttachmentID: "safe-shapes"}}}}, Attachments: []ProbeAttachment{{ID: "safe-shapes", MediaType: "image/png", Encoding: "base64", Data: base64.StdEncoding.EncodeToString(imageData)}}})
	pdf := makeItem("inspection/pdf/1", "A11.image_pdf", "安全 PDF 阅读", "inspection_function", "只输出 PDF 中的六位 Code。", "314159", ProbeRequest{Messages: []ProbeMessage{{Role: "user", Content: []ProbeContent{{Type: "text", Text: "只输出 PDF 中的六位 Code。"}, {Type: "document", AttachmentID: "safe-note"}}}}, Attachments: []ProbeAttachment{{ID: "safe-note", MediaType: "application/pdf", Encoding: "base64", Data: base64.StdEncoding.EncodeToString(inspectionSafePDF())}}})
	pdf.Platforms = []string{"anthropic", "openai", "gemini", "antigravity"}
	permission := makeItem("inspection/model-permission/1", "A04", "上架型号权限", "inspection_authenticity", "只输出 READY。", "READY", ProbeRequest{})
	boundary := makeItem("inspection/context-boundary/1", "A05", "上下文边界抽样", "inspection_authenticity", "只输出 READY。", "READY", ProbeRequest{})
	thinking := makeItem("inspection/thinking/1", "A11.thinking", "推理功能观测", "inspection_function", "计算 17 乘以 23，只输出整数。", "391", ProbeRequest{Effort: "high"})
	return []Item{cache, img, pdf, permission, boundary, thinking}
}

func inspectionSafeImage() []byte {
	picture := image.NewRGBA(image.Rect(0, 0, 192, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 192; x++ {
			c := color.RGBA{255, 255, 255, 255}
			if y >= 16 && y < 48 && (x >= 16 && x < 48 || x >= 80 && x < 112 || x >= 144 && x < 176) {
				c = color.RGBA{0, 80, 255, 255}
			}
			picture.SetRGBA(x, y, c)
		}
	}
	var out bytes.Buffer
	_ = png.Encode(&out, picture)
	return out.Bytes()
}

func inspectionSafePDF() []byte {
	stream := "BT /F1 18 Tf 72 720 Td (Token Market Safe Note) Tj 0 -30 Td (Code: 314159) Tj ET\n"
	objects := []string{"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>", "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>", fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(stream), stream)}
	var b strings.Builder
	b.WriteString("%PDF-1.4\n")
	offsets := []int{0}
	for i, obj := range objects {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	offset := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, n := range offsets[1:] {
		fmt.Fprintf(&b, "%010d 00000 n \n", n)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), offset)
	return []byte(b.String())
}

// 补充探针仍使用题库稳定键与冻结快照，旧批次不会重新生成。
func addInspectionProbes(plan *executionSnapshot, p *Policy, st InspectionSettings) {
	if plan.Mode != "full" {
		return
	}
	var extra []plannedInstance
	anchors := 0
	strengthRule, _ := inspection.Effective("D06", st)
	for i := range plan.Instances {
		instance := &plan.Instances[i]
		if strengthRule.Enabled && (st.Mode == "formal" || containsAny(p.Suites, "inspection_function")) && instance.CheckKey == "D07" && len(instance.Steps) == 1 && strings.Contains(instance.GeneratorVersion, "/") && anchors < 10 {
			low := *instance
			low.InstanceIdx += 1000
			low.CheckKey = "D06"
			low.Steps = append([]plannedStep(nil), instance.Steps...)
			low.Steps[0].Request.Effort, low.Steps[0].Request.ReasoningEffort = "low", ""
			if strings.HasPrefix(low.Steps[0].Request.Model, "claude-") && !claudeAdaptive(low.Steps[0].Request.Model) {
				low.Steps[0].Request.Extra = map[string]any{"thinking": map[string]any{"type": "enabled", "budget_tokens": 1024}}
			}
			extra = append(extra, low)
			anchors++
		}
		if instance.CheckKey == "A05" {
			rule, _ := inspection.Effective("A05", st)
			target := int(rule.Thresholds["probe_tokens"])
			if target < 8192 || target > 524288 {
				target = 262145
			}
			row := "The garden record has green trees, clear water, and smooth stones.\n"
			text := strings.Repeat(row, target/(estimateContextTokens(row))+1) + "\n只输出 READY。"
			head, tail := instance.Seed[:8], instance.Seed[8:16]
			text = "开头标记 HEAD=" + head + "。\n" + text + "\n末尾标记 TAIL=" + tail + "。只输出一行 HEAD=开头标记值,TAIL=末尾标记值，不输出说明。"
			instance.Steps[0].Request.Prompt = text
			instance.Steps[0].Expected = Grader{Type: "fields_exact", Answer: "HEAD=" + head + ",TAIL=" + tail, GraderOptions: GraderOptions{Fields: map[string]string{"HEAD": head, "TAIL": tail}, Inline: true}}
			instance.Steps[0].Answer = instance.Steps[0].Expected.Answer
			instance.Parameters = map[string]any{"estimated_input_tokens": estimateContextTokens(text), "target_tokens": target}
			instance.GeneratorVersion = "context_boundary/1"
			instance.Steps[0].Request.Expected = ProbeExpected{Outcome: "reject", HTTPStatus: []int{400, 413}, ErrorCodes: []string{"context_length_exceeded", "max_input_tokens", "input_too_long"}}
		}
		if instance.CheckKey == "A04" {
			for n, model := range p.Models {
				if model == instance.Steps[0].Request.Model {
					continue
				}
				copyInstance := *instance
				copyInstance.InstanceIdx = n + 1
				copyInstance.Steps = append([]plannedStep(nil), instance.Steps...)
				copyInstance.Steps[0].Request.Model = model
				extra = append(extra, copyInstance)
			}
		}
	}
	plan.Instances = append(plan.Instances, extra...)
	for i := range plan.Instances {
		plan.Instances[i].Hash = ""
		sum := sha256.Sum256(mustJSON(plan.Instances[i]))
		plan.Instances[i].Hash = hex.EncodeToString(sum[:])
	}
}

// 暂停后的可用率复检补足本次新鲜样本，不能等待已经停止的生产流量恢复。
func (e *Engine) addAvailabilityRecoveryProbes(plan *executionSnapshot, batch string, count int) error {
	if count < 1 || count+len(plan.Instances) > 200 {
		return fmt.Errorf("可用率恢复所需 %d 个样本超出本批次实例上限，请调整检测策略", count)
	}
	for _, it := range plan.Instances {
		if it.CheckKey != "A04" || len(it.Steps) != 1 {
			continue
		}
		for i := 0; i < count; i++ {
			copyInstance := it
			copyInstance.InstanceIdx, copyInstance.CheckKey = 2000+i, "D13"
			copyInstance.GeneratorVersion = "availability_recovery/1"
			copyInstance.Seed = e.instanceSeed(batch, it.ItemID, 2000+i, copyInstance.GeneratorVersion)
			copyInstance.Steps = append([]plannedStep(nil), it.Steps...)
			copyInstance.Steps[0].Request.Prompt = "只输出 READY。检测编号：" + copyInstance.Seed[:16]
			copyInstance.Steps[0].Request.Messages = nil
			copyInstance.Hash = ""
			sum := sha256.Sum256(mustJSON(copyInstance))
			copyInstance.Hash = hex.EncodeToString(sum[:])
			plan.Instances = append(plan.Instances, copyInstance)
		}
		return nil
	}
	return fmt.Errorf("可用率恢复缺少可用的型号权限探针，请启用 A04 或恢复其题库项")
}
