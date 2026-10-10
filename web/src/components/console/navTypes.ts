import type { ConsoleIconName } from '@/components/icons/ConsoleIcon.vue'

export interface NavNode {
  key: string
  text: string
  icon?: ConsoleIconName
  /** 叶子节点路由；有 children 时可省略 */
  to?: string
  children?: NavNode[]
}
