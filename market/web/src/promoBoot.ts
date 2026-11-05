// 推广链接落地：读 ?ch= 留存渠道并上报一次访问；失败一律不影响页面。
// 必须是 main.ts 的第一个 import：ES 模块按 import 顺序求值，这里要赶在 ./router 的 createWebHistory()
// 读取地址栏之前去掉 ch，否则路由首跳会把带 ch 的地址写回地址栏，用户刷新或转发链接时会重复上报。
import { captureFromUrl } from './utils/promo'

captureFromUrl()
