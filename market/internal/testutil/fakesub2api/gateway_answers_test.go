package fakesub2api

import (
	"context"
	"encoding/json"
	"testing"
)

func TestRegisteredAnswersKeepAccountFaultsLocal(t *testing.T) {
	s := NewUnstarted(nil, "")
	body := []byte(`{"model":"测试型号","messages":[{"role":"user","content":"现场题目"}]}`)
	if err := s.RegisterProbeAnswer(body, GatewayResponse{Text: "标准答案", ToolCalls: []GatewayToolCall{{Name: "本地工具", Arguments: map[string]any{"id": 7}}}}); err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatal(err)
	}
	req := parseGatewayRequest(parsed, "")
	s.SetAccountIncorrectAnswers(1, true)
	bad, ok := s.gatewayResponse(context.Background(), 1, req)
	good, goodOK := s.gatewayResponse(context.Background(), 2, req)
	if !ok || !goodOK || bad.Text != "错误答案" || len(bad.ToolCalls) != 0 || good.Text != "标准答案" || len(good.ToolCalls) != 1 {
		t.Fatalf("账号错误影响其他账号：%+v %+v", bad, good)
	}
}
