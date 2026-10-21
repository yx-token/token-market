// Package gateway 是市场侧对 sub2api 写能力的端口。
//
// 阶段 1：HTTP 适配委托现有 adminclient，出站队列与影子用户保持不变，
// 便于继续跟社区 sub2api 版本合并。
//
// 后续若同仓进程内调用，新增实现满足 Admin 即可，不必改业务入队代码。
// 禁止在本包实现计费/调度内核逻辑。
package gateway
