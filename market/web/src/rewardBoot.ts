// loxo 任务票据落地：读 ?reward_ticket= 留存并从地址栏去掉；浏览任务按需加载 loxo SDK。失败一律不影响页面。
// 与 promoBoot 一样必须排在 ./router 之前求值：要赶在 createWebHistory() 读取地址栏之前去掉票据。
import { captureRewardTicketFromUrl, resolveRewardLanding, restoreRewardSdk } from './utils/rewardTicket'

const ticket = captureRewardTicketFromUrl()
if (ticket) void resolveRewardLanding(ticket)
else restoreRewardSdk()
