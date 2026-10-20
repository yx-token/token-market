package httpapi

import "strings"

// marketV1Pattern 把闲猫管理路由挂到 sub2api 增量命名空间：
// GET /api/admin/vendors → GET /api/v1/admin/market/vendors
// GET /api/admin/sub2api/status → GET /api/v1/admin/market/status
// 已迁入 backend/idlecat 的接口返回空串，交给 /api/v1/ 反代到真实引擎。
func marketV1Pattern(pattern string) string {
	method, path, ok := strings.Cut(pattern, " ")
	if !ok || !strings.HasPrefix(path, "/api/admin/") {
		return ""
	}
	rest := strings.TrimPrefix(path, "/api/admin/")
	if rest == "" || strings.HasPrefix(rest, "engine/") {
		return ""
	}
	if rest == "sub2api/status" {
		rest = "status"
	}
	if marketNativeOnEngine(method, rest) {
		return ""
	}
	return method + " /api/v1/admin/market/" + rest
}

// marketNativeOnEngine 与引擎 /api/v1/admin/market 对齐。
// 系统升级仍留在 market-api（依赖本进程的更新控制器）。其余管理接口由引擎执行。
func marketNativeOnEngine(method, rest string) bool {
	if strings.HasPrefix(rest, "system/") {
		return false
	}
	switch method {
	case "GET", "POST", "PUT", "DELETE", "PATCH":
		return rest != ""
	default:
		return false
	}
}
