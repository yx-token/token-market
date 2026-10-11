package fakesub2api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// RegisterProbeAnswer 用完整请求登记离线答案，支持动态多轮；不会执行生成的程序。
func (s *Server) RegisterProbeAnswer(body []byte, response GatewayResponse) error {
	var object map[string]any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&object); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.registeredAnswers == nil {
		s.registeredAnswers = map[string]GatewayResponse{}
	}
	response.ToolCalls = cloneCalls(response.ToolCalls)
	s.registeredAnswers[gatewayBodyKey(object)] = response
	return nil
}

func gatewayBodyKey(body map[string]any) string {
	raw, _ := json.Marshal(body)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// SetAccountIncorrectAnswers 仅影响已登记的题目，便于验证影子模式隔离。
func (s *Server) SetAccountIncorrectAnswers(accountID int64, incorrect bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.incorrectAnswers == nil {
		s.incorrectAnswers = map[int64]bool{}
	}
	s.incorrectAnswers[accountID] = incorrect
}
