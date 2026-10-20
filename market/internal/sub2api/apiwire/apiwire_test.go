package apiwire

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDoParsesNumericAndStringCodes(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		wantErr    bool
		wantCode   int
		wantString string
	}{
		{"success", 200, `{"code":0,"message":"success","data":{"v":1}}`, false, 0, ""},
		{"numeric error", 400, `{"code":400,"message":"bad"}`, true, 400, ""},
		// sub2api 的部分中间件用字符串错误码，例如管理员合规确认
		{"string error", 423, `{"code":"ADMIN_COMPLIANCE_ACK_REQUIRED","message":"ack required","metadata":{"version":"v1"}}`, true, 0, "ADMIN_COMPLIANCE_ACK_REQUIRED"},
		{"missing code", 200, `{"message":"weird"}`, true, 0, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			var out struct{ V int }
			err := New(srv.URL, time.Second, nil).Do(context.Background(), http.MethodGet, "/x", nil, nil, &out)
			if !tc.wantErr {
				if err != nil || out.V != 1 {
					t.Fatalf("err=%v out=%+v", err, out)
				}
				return
			}
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("expected APIError, got %v", err)
			}
			if apiErr.Code != tc.wantCode || apiErr.ErrorCode != tc.wantString || apiErr.HTTPStatus != tc.status {
				t.Fatalf("got %+v", apiErr)
			}
		})
	}
}
