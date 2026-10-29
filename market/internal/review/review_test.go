package review

import "testing"

func TestMaskEmail(t *testing.T) {
	if got := maskEmail("ab@x.com"); got != "a***@x.com" {
		t.Fatalf("got %q", got)
	}
	if got := maskEmail(""); got != "买家" {
		t.Fatalf("empty got %q", got)
	}
}

func TestNormalizeTags(t *testing.T) {
	tags, err := normalizeTags([]string{"稳定", "便宜", "稳定"})
	if err != nil || len(tags) != 2 {
		t.Fatalf("%v %#v", err, tags)
	}
	if _, err := normalizeTags([]string{"不存在的标签"}); err == nil {
		t.Fatal("expected invalid tag")
	}
}
