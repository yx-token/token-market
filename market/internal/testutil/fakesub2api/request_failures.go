package fakesub2api

// FailNext 让指定接口下一次返回给定状态码，覆盖尚未调用底层写入的失败场景。
// 同一接口可连续配置多次，按调用顺序消耗；不影响其他接口。
func (s *Server) FailNext(method, path string, status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := method + " " + path
	s.nextFailures[key] = append(s.nextFailures[key], status)
}

func (s *Server) nextFailure(method, path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := method + " " + path
	statuses := s.nextFailures[key]
	if len(statuses) == 0 {
		return 0
	}
	status := statuses[0]
	if len(statuses) == 1 {
		delete(s.nextFailures, key)
	} else {
		s.nextFailures[key] = statuses[1:]
	}
	return status
}
