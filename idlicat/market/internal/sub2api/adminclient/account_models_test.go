package adminclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestGetAccountModelsPrefersSyncAndFallsBack(t *testing.T) {
	tests := []struct {
		name       string
		syncStatus int
		syncData   any
		getStatus  int
		getData    any
		want       []string
		wantCalls  []string
		wantErr    bool
	}{
		{"同步目录", 200, map[string]any{"models": []string{" gpt-6-astra ", "gpt-6-astra", "", "gpt-6.1-sol"}}, 200, nil, []string{"gpt-6-astra", "gpt-6.1-sol"}, []string{"POST"}, false},
		{"同步空目录", 200, map[string]any{"models": []string{}}, 200, nil, []string{}, []string{"POST"}, false},
		{"同步失败查询回退", 502, nil, 200, []map[string]string{{"id": "gpt-6-astra"}, {"id": "gpt-6-astra"}, {"id": "gpt-6.1-sol"}}, []string{"gpt-6-astra", "gpt-6.1-sol"}, []string{"POST", "GET"}, false},
		{"同步不支持查询回退", 400, nil, 200, []map[string]string{{"id": "claude-sonnet-4-5"}}, []string{"claude-sonnet-4-5"}, []string{"POST", "GET"}, false},
		{"同步格式异常查询回退", 200, []string{"gpt-6-astra"}, 200, []map[string]string{{"id": "claude-sonnet-4-5"}}, []string{"claude-sonnet-4-5"}, []string{"POST", "GET"}, false},
		{"两个接口都失败", 502, nil, 503, nil, nil, []string{"POST", "GET"}, true},
		{"不支持同步且查询暂时失败", 400, nil, 503, nil, nil, []string{"POST", "GET"}, true},
		{"查询格式异常", 502, nil, 200, []string{"gpt-6-astra"}, nil, []string{"POST", "GET"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("x-api-key") != "admin-key" {
					t.Errorf("模型目录请求未携带管理员凭证")
				}
				calls = append(calls, r.Method)
				status, data := tt.syncStatus, tt.syncData
				wantPath := "/api/v1/admin/accounts/42/models/sync-upstream"
				if r.Method == http.MethodGet {
					status, data = tt.getStatus, tt.getData
					wantPath = "/api/v1/admin/accounts/42/models"
				}
				if r.URL.Path != wantPath {
					t.Errorf("模型目录路径 = %q，期望 %q", r.URL.Path, wantPath)
				}
				w.WriteHeader(status)
				code := 0
				if status != http.StatusOK {
					code = status
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "message": tt.name, "data": data})
			}))
			t.Cleanup(srv.Close)
			c := New(srv.URL, "admin-key", time.Second)
			models, err := c.GetAccountModels(context.Background(), 42)
			if (err != nil) != tt.wantErr {
				t.Fatalf("目录错误 = %v", err)
			}
			if !reflect.DeepEqual(models, tt.want) || !reflect.DeepEqual(calls, tt.wantCalls) {
				t.Fatalf("目录 = %v，请求 = %v；期望目录 %v，请求 %v", models, calls, tt.want, tt.wantCalls)
			}
			if tt.getStatus == 503 && !IsRetryable(err) {
				t.Fatalf("两个接口都失败时应保留可重试的 sub2api 错误：%v", err)
			}
			calls = nil
			_, fromUpstream, err := c.GetAccountModelCatalog(context.Background(), 42)
			wantUpstream := tt.syncStatus == http.StatusOK && len(tt.wantCalls) == 1
			if (err != nil) != tt.wantErr || fromUpstream != wantUpstream {
				t.Fatalf("目录来源 = %v，期望 %v，错误 %v", fromUpstream, wantUpstream, err)
			}
		})
	}
}

func TestGetAccountModelsWriteGateFallsBackToRead(t *testing.T) {
	c, rec := newServer(t, 200, `{"code":0,"data":[{"id":"claude-sonnet-4-5"}]}`, nil)
	c.writeGate = func(context.Context) error { return ErrWritesPaused }
	models, err := c.GetAccountModels(context.Background(), 42)
	if err != nil || !reflect.DeepEqual(models, []string{"claude-sonnet-4-5"}) || rec.method != http.MethodGet {
		t.Fatalf("同步门控后仍应允许目录只读回退：目录 %v，请求 %s，错误 %v", models, rec.method, err)
	}
}

func TestSetAccountModelWhitelistUsesBulkMerge(t *testing.T) {
	c, rec := newServer(t, 200, `{"code":0,"data":{"success":1,"failed":0,"success_ids":[42],"failed_ids":[],"results":[{"account_id":42,"success":true}]}}`, nil)
	if err := c.SetAccountModelWhitelist(context.Background(), 42, []string{"gpt-6-astra", " gpt-6.1-sol ", "gpt-6-astra"}); err != nil {
		t.Fatal(err)
	}
	if rec.method != http.MethodPost || rec.path != "/api/v1/admin/accounts/bulk-update" {
		t.Fatalf("白名单应走合并接口：%s %s", rec.method, rec.path)
	}
	want := map[string]any{
		"account_ids": []any{float64(42)},
		"credentials": map[string]any{"model_mapping": map[string]any{"gpt-6-astra": "gpt-6-astra", "gpt-6.1-sol": "gpt-6.1-sol"}},
	}
	if !reflect.DeepEqual(rec.body, want) {
		t.Fatalf("白名单请求 = %#v，期望 %#v", rec.body, want)
	}
}

func TestSetAccountModelWhitelistDoesNotWriteUndeclaredOrInvalidList(t *testing.T) {
	for _, models := range [][]string{nil, {}, {""}, {"   "}} {
		c, rec := newServer(t, 200, `{"code":0,"data":{"success":1}}`, nil)
		err := c.SetAccountModelWhitelist(context.Background(), 42, models)
		if models == nil && err != nil {
			t.Fatalf("未声明白名单应保留账号：%v", err)
		}
		if models != nil && err == nil {
			t.Fatalf("无效白名单应拒绝：%v", models)
		}
		if rec.method != "" {
			t.Fatalf("无效或未声明的白名单不应发请求：%v", models)
		}
	}
}

func TestSetAccountModelWhitelistRejectsFailedBulkResult(t *testing.T) {
	for _, data := range []string{
		`{"success":0,"failed":1,"failed_ids":[42],"results":[{"account_id":42,"success":false,"error":"账号不可修改"}]}`,
		`{"success":1,"failed":0,"failed_ids":[42]}`,
		`{"success":1,"failed":0,"success_ids":[43]}`,
		`{"success":1,"failed":0,"results":[{"account_id":42,"success":false,"error":"账号不可修改"}]}`,
		`{"success":1,"failed":0,"results":[{"account_id":43,"success":true}]}`,
		`{"success":2,"failed":0}`,
		`{"success":0,"failed":0}`,
		`"非法结果"`,
	} {
		c, _ := newServer(t, 200, `{"code":0,"data":`+data+`}`, nil)
		if err := c.SetAccountModelWhitelist(context.Background(), 42, []string{"gpt-6-astra"}); err == nil {
			t.Fatalf("不能把批量失败当成功：%s", data)
		}
	}
	c, _ := newServer(t, 503, `{"code":503,"message":"上游暂时不可用"}`, nil)
	if err := c.SetAccountModelWhitelist(context.Background(), 42, []string{"gpt-6-astra"}); !IsRetryable(err) {
		t.Fatalf("应保留写入接口的错误：%v", err)
	}
}

func TestGetAccountModelsRetainsBothFailures(t *testing.T) {
	c, _ := newServer(t, 502, `{"code":502,"message":"目录故障"}`, nil)
	_, err := c.GetAccountModels(context.Background(), 42)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || !strings.Contains(err.Error(), "同步模型目录失败") || !strings.Contains(err.Error(), "查询账号模型失败") {
		t.Fatalf("应保留两个目录错误：%v", err)
	}
}
