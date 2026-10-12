/** 识别整段文本是否为 JSON（可包在 ```json 代码块里），供检测明细把 JSON 回答渲染成结构。 */
export type JsonDetection =
  | { kind: 'json'; value: object; fenced: boolean }
  /** 形似 JSON 但解析失败（常见于回答被截断），应按原文等宽显示，不能当 Markdown 渲染 */
  | { kind: 'broken'; fenced: boolean }

const fence = /^```[ \t]*(?:json|JSON)?[ \t]*\r?\n([\s\S]*?)\r?\n?```$/

export function detectJson(text: string | null | undefined): JsonDetection | null {
  let body = (text ?? '').trim()
  let fenced = false
  const m = body.match(fence)
  if (m) {
    body = m[1].trim()
    fenced = true
  } else if (/^```[ \t]*(?:json|JSON)[ \t]*\r?\n/.test(body)) {
    // 只有开头没有结尾的 json 代码块：多半是回答被截断
    return { kind: 'broken', fenced: true }
  }
  if (!body.startsWith('{') && !body.startsWith('[')) return null
  try {
    const value: unknown = JSON.parse(body)
    if (value !== null && typeof value === 'object') return { kind: 'json', value, fenced }
    return null
  } catch {
    return { kind: 'broken', fenced }
  }
}
