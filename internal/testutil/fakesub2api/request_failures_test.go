package fakesub2api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFailNextReturnsBeforeWrite(t *testing.T) {
	s := NewUnstarted(nil, "admin-key")
	path := "/api/v1/admin/accounts/bulk-update"
	s.FailNext(http.MethodPost, path, http.StatusServiceUnavailable)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"account_ids":[42]}`))
	writes := 0
	handler := func(http.ResponseWriter, *http.Request) (any, error) {
		writes++
		return nil, nil
	}
	s.serve(w, r, handler)
	if w.Code != http.StatusServiceUnavailable || writes != 0 {
		t.Fatalf("模拟失败仍然执行写入：状态 %d，写入次数 %d", w.Code, writes)
	}
	w = httptest.NewRecorder()
	s.serve(w, r, handler)
	if w.Code != http.StatusOK || writes != 1 {
		t.Fatalf("单次故障没有恢复：状态 %d，写入次数 %d", w.Code, writes)
	}
}

func TestFailNextPreservesOtherRoutesAndQueue(t *testing.T) {
	s := NewUnstarted(nil, "admin-key")
	s.FailNext(http.MethodPost, "/模型写入", 503)
	s.FailNext(http.MethodPost, "/模型写入", 502)
	if got := s.nextFailure(http.MethodGet, "/模型写入"); got != 0 {
		t.Fatalf("其他方法被误伤：%d", got)
	}
	if got := s.nextFailure(http.MethodPost, "/其他接口"); got != 0 {
		t.Fatalf("其他路径被误伤：%d", got)
	}
	for _, want := range []int{503, 502, 0} {
		if got := s.nextFailure(http.MethodPost, "/模型写入"); got != want {
			t.Fatalf("故障顺序 = %d，期望 %d", got, want)
		}
	}
}
