import MarkdownIt from 'markdown-it'

// 关闭原始 HTML：发布说明来自外部仓库，HTML 标签按文本转义；危险协议的链接由 markdown-it 默认拦截
const md = new MarkdownIt({ html: false, linkify: true, breaks: true })

const renderLinkOpen = md.renderer.rules.link_open ?? ((tokens, idx, options, _env, self) => self.renderToken(tokens, idx, options))
md.renderer.rules.link_open = (tokens, idx, options, env, self) => {
  tokens[idx].attrSet('target', '_blank')
  tokens[idx].attrSet('rel', 'noopener noreferrer')
  return renderLinkOpen(tokens, idx, options, env, self)
}

/** 把 Markdown 渲染为安全的 HTML，供 v-html 使用 */
export function renderMarkdown(src: string): string {
  return md.render(src)
}
