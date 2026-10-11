package quality

import (
	"strings"
	"testing"
)

func TestBankRevisionPreservesVersionOne(t *testing.T) {
	seed := strings.Repeat("0123456789abcdef", 4)
	for _, template := range []string{"zebra", "constraints"} {
		old, err := generateBankProbe(generatorDefinition{TemplateID: template, Version: "1"}, seed)
		if err != nil {
			t.Fatal(err)
		}
		updated, err := generateBankProbe(generatorDefinition{TemplateID: template, Version: "2"}, seed)
		if err != nil {
			t.Fatal(err)
		}
		if contextHash(t, old) == contextHash(t, updated) {
			t.Fatalf("%s 的新版语义须与旧版本分开", template)
		}
		if template == "constraints" && !strings.Contains(updated.Request.Prompt, "自由写作") {
			t.Fatal("版本二须使用自由写作")
		}
	}
	for _, key := range []string{"inspection/fact/1", "inspection/abstain/1", "inspection/output/1", "inspection/zebra/1", "inspection/constraints/en/1", "inspection/constraints/zh/1"} {
		if !obsoleteInspectionBuiltin(&key) {
			t.Fatalf("旧内置应由新版选择器跳过：%s", key)
		}
	}
	if obsoleteInspectionBuiltin(nil) {
		t.Fatal("管理员自建题不能被旧键过滤")
	}
	for _, it := range inspectionBuiltinItems() {
		if obsoleteInspectionBuiltin(it.BuiltinKey) {
			t.Fatalf("新目录仍包含过期键：%s", *it.BuiltinKey)
		}
	}
}
