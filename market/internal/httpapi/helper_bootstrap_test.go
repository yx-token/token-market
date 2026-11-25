package httpapi

import (
	"strings"
	"testing"
)

func TestContentDispositionAttachmentUsesChineseFilename(t *testing.T) {
	got := contentDispositionAttachment(helperDownloadWindows)
	if !strings.Contains(got, `filename="xianmao-helper-windows-setup.exe"`) {
		t.Fatalf("missing ascii fallback: %s", got)
	}
	if !strings.Contains(got, "filename*=UTF-8''") {
		t.Fatalf("missing UTF-8 filename*: %s", got)
	}
	if !strings.Contains(got, "%E9%97%B2%E7%8C%AB%E5%8A%A9%E6%89%8B.exe") {
		t.Fatalf("missing percent-encoded 闲猫助手.exe: %s", got)
	}
}

func TestAsciiFilenameFallback(t *testing.T) {
	if got := asciiFilenameFallback(helperDownloadMacOS); got != helperAssetMacOS {
		t.Fatalf("macos fallback: %s", got)
	}
	if got := asciiFilenameFallback(helperDownloadWindows); got != helperAssetWindows {
		t.Fatalf("windows fallback: %s", got)
	}
}
