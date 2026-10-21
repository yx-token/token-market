/** SearchSelect 的选项类型；单独成文件，便于表单页与测试从 .vue 里直接引用。 */
export interface SelectOption {
  value: string
  text: string
  /** 已失效 / 未注册的选项，仅提示 */
  stale?: boolean
  /** 过滤用的关键字，缺省用 value 与 text */
  keywords?: string
}