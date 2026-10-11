package edge

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DueGin/token-market/internal/edgecfg"
)

func TestAttemptHTTPErrorClasses(t *testing.T) {
	for _, row := range []struct {
		name string
		code int
		body string
		want string
	}{{"本地并发", 429, `{"error":{"code":"gateway_concurrency_limit","type":"rate_limit_error"}}`, "platform"},
		{"本地队列", 429, `{"error":{"code":"gateway_queue_full"}}`, "platform"},
		{"客户端版本", 400, `{"error":{"message":"claude_code_version_too_old"}}`, "platform"},
		{"平台余额", 403, `{"code":"INSUFFICIENT_BALANCE","message":"余额不足"}`, "platform"},
		{"嵌套平台余额", 403, `{"error":{"code":"INSUFFICIENT_BALANCE"}}`, "platform"},
		{"小写余额码", 403, `{"error":{"code":"insufficient_balance"}}`, "http_4xx"},
		{"余额码错误状态", 402, `{"error":{"code":"INSUFFICIENT_BALANCE"}}`, "http_4xx"},
		{"余额码限流状态", 429, `{"error":{"code":"INSUFFICIENT_BALANCE"}}`, "rate_limited"},
		{"余额码故障状态", 500, `{"error":{"code":"INSUFFICIENT_BALANCE"}}`, "http_5xx"},
		{"余额类型不是码", 403, `{"error":{"type":"INSUFFICIENT_BALANCE"}}`, "http_4xx"},
		{"余额状态不是码", 403, `{"error":{"status":"INSUFFICIENT_BALANCE"}}`, "http_4xx"},
		{"余额键不是码", 403, `{"error":{"errorKey":"INSUFFICIENT_BALANCE"}}`, "http_4xx"},
		{"余额消息不是码", 403, `{"error":{"message":"INSUFFICIENT_BALANCE"}}`, "http_4xx"},
		{"余额码前缀", 403, `{"error":{"code":"VENDOR_INSUFFICIENT_BALANCE"}}`, "http_4xx"},
		{"余额码后缀", 403, `{"error":{"code":"INSUFFICIENT_BALANCE_EXHAUSTED"}}`, "http_4xx"},
		{"余额码空格", 403, `{"error":{"code":" INSUFFICIENT_BALANCE "}}`, "http_4xx"},
		{"厂商余额提示", 402, `{"error":{"message":"Insufficient balance"}}`, "http_4xx"},
		{"厂商中文余额", 403, `{"error":{"message":"余额不足"}}`, "http_4xx"},
		{"厂商余额不足码", 403, `{"error":{"code":"balance_not_enough"}}`, "http_4xx"},
		{"厂商余额反序码", 403, `{"error":{"code":"balance_insufficient"}}`, "http_4xx"},
		{"厂商信用余额", 403, `{"error":{"message":"insufficient credit balance"}}`, "http_4xx"},
		{"厂商余额限流", 429, `{"error":{"message":"余额不足"}}`, "rate_limited"},
		{"厂商余额参数类型", 400, `{"error":{"type":"invalid_request_error","message":"insufficient credit balance"}}`, "http_4xx"},
		{"嵌套厂商余额拒绝", 403, `{"error":{"cause":{"errorKey":"insufficient_balance"}}}`, "http_4xx"},
		{"嵌套厂商余额", 500, `{"error":{"cause":{"errorKey":"insufficient_balance"}}}`, "http_5xx"},
		{"普通并发提示", 429, `{"error":{"message":"Concurrency limit exceeded"}}`, "rate_limited"},
		{"普通并发故障", 503, `{"error":{"message":"Concurrency limit exceeded"}}`, "http_5xx"},
		{"限流", 429, `{"error":{"type":"rate_limit_error"}}`, "rate_limited"},
		{"过载", 529, `{"error":{"type":"overloaded_error"}}`, "rate_limited"},
		{"无正文限流", 429, "", "rate_limited"},
		{"错误参数", 400, `{"error":{"type":"invalid_request_error"}}`, "configuration"},
		{"不支持参数", 400, `{"error":{"code":"unsupported_parameter"}}`, "configuration"},
		{"Gemini参数", 400, `{"error":{"status":"INVALID_ARGUMENT"}}`, "configuration"},
		{"转发服务故障", 503, `{"error":{"type":"api_error"}}`, "http_5xx"},
		{"无正文服务故障", 502, "", "http_5xx"},
		{"未知错误", 400, `{"error":"bad request"}`, "unknown"},
		{"普通权限不足", 403, `{"error":{"message":"permission denied"}}`, "unknown"},
		{"错误HTML", 400, "<html>bad request</html>", "unknown"},
		{"不解析请求回显", 400, `{"request":{"prompt":"gateway_concurrency_limit"}}`, "unknown"},
		{"不解析余额请求回显", 403, `{"request":{"code":"INSUFFICIENT_BALANCE"}}`, "unknown"},
		{"成功不解析正文", 200, `{"error":{"code":"gateway_queue_full"}}`, ""}} {
		t.Run(row.name, func(t *testing.T) {
			if got := attemptHTTPClass(row.code, []byte(row.body)); got != row.want {
				t.Fatalf("分类错误：%s，预期 %s", got, row.want)
			}
		})
	}
}

func TestAttemptErrorObservationPreservesProxyResponse(t *testing.T) {
	for _, row := range []struct {
		code int
		body string
		want string
	}{{400, `{"error":{"code":"unsupported_parameter","message":"正文原样透传"}}`, "configuration"},
		{429, `{"error":{"code":"gateway_concurrency_limit"}}`, "platform"},
		{403, `{"error":{"message":"insufficient credit balance"}}`, "http_4xx"},
		{503, `{"error":{"type":"api_error"}}`, "http_5xx"},
		{529, `{"error":{"type":"overloaded_error"}}`, "rate_limited"}} {
		t.Run(row.want, func(t *testing.T) {
			e := newEnv(t)
			logger := &AttemptLogger{ch: make(chan Attempt, 10)}
			e.srv.attempts = logger
			e.gateway.byKey["tmik_a"] = func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(row.code)
				_, _ = io.WriteString(w, row.body)
			}
			e.put(t, "tm-buyer", edgecfg.KeyConfig{BuyerKeyID: 7, Status: "active", Hops: []edgecfg.Hop{e.hop(t, 1, 1, "tmik_a")}})
			rec := e.do(http.MethodPost, "/v1/messages", "tm-buyer", `{}`)
			if rec.Code != row.code || rec.Body.String() != row.body {
				t.Fatalf("新增观测改变原响应：%d %q", rec.Code, rec.Body.String())
			}
			a := <-logger.ch
			if a.ErrorClass != row.want || a.HTTPStatus != row.code || !a.Billable || a.ListingID != 1 {
				t.Fatalf("尝试元数据错误：%+v", a)
			}
			if retryableStatus(row.code) {
				if a.EndReason != "retry" || a.Committed {
					t.Fatalf("重试和提交口径改变：%+v", a)
				}
			} else if a.EndReason != "completed" || !a.Committed {
				t.Fatalf("不可重试错误的旧口径改变：%+v", a)
			}
			raw, err := json.Marshal(a)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), row.body) || strings.Contains(string(raw), "正文原样透传") {
				t.Fatal("尝试记录不能保存错误正文")
			}
		})
	}
}

func TestAttemptErrorCaptureIsBoundedAndTransparent(t *testing.T) {
	var capture attemptErrorCapture
	text := strings.Repeat("甲", attemptErrorBodyLimit)
	got, err := io.ReadAll(io.TeeReader(strings.NewReader(text), &capture))
	if err != nil || string(got) != text || len(capture.data) != attemptErrorBodyLimit {
		t.Fatalf("有限捕获不能截断透传正文：正文=%d，捕获=%d，错误=%v", len(got), len(capture.data), err)
	}
	var old Attempt
	if err := json.Unmarshal([]byte(`{"http_status":503,"end_reason":"retry"}`), &old); err != nil || old.ErrorClass != "" {
		t.Fatalf("旧暂存记录须保留未知分类的安全默认值：%+v %v", old, err)
	}
}

func TestAttemptNetworkClasses(t *testing.T) {
	for _, row := range []struct {
		err  error
		want string
	}{{context.DeadlineExceeded, "timeout"}, {&net.DNSError{IsTimeout: true}, "timeout"}, {errors.New("连接中断"), "network"}} {
		if got := attemptNetworkClass(row.err); got != row.want {
			t.Fatalf("网络错误分类：%s，预期 %s", got, row.want)
		}
	}
	e := newEnv(t)
	logger := &AttemptLogger{ch: make(chan Attempt, 10)}
	e.srv.attempts = logger
	e.srv.cfg.FirstByteTimeout = 30 * time.Millisecond
	e.gateway.byKey["tmik_a"] = func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(300 * time.Millisecond):
		}
	}
	e.put(t, "tm-buyer", edgecfg.KeyConfig{BuyerKeyID: 7, Status: "active", Hops: []edgecfg.Hop{e.hop(t, 1, 1, "tmik_a")}})
	_ = e.do(http.MethodPost, "/v1/messages", "tm-buyer", `{}`)
	if a := <-logger.ch; a.ErrorClass != "timeout" || a.EndReason != "timeout" {
		t.Fatalf("截止时间必须只新增 timeout 分类：%+v", a)
	}
}

func TestBuyerLocalFailureDoesNotCreateSellerObservation(t *testing.T) {
	e := newEnv(t)
	logger := &AttemptLogger{ch: make(chan Attempt, 10)}
	e.srv.attempts = logger
	_ = e.do(http.MethodPost, "/v1/messages", "tm-unknown", `{}`)
	if len(logger.ch) != 0 {
		t.Fatal("本地买家授权失败不能生成卖家可用率观测")
	}
}
