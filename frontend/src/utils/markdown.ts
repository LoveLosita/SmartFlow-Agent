import MarkdownIt from 'markdown-it'
import hljs from 'highlight.js/lib/common'
import 'highlight.js/styles/github.css'

function escapeHtml(input: string) {
  return input
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#39;')
}

function renderHighlightedCode(sourceCode: string, language: string) {
  const normalizedLanguage = language.trim()
  const safeLanguageClass = normalizedLanguage ? ` language-${escapeHtml(normalizedLanguage)}` : ''

  try {
    if (normalizedLanguage && hljs.getLanguage(normalizedLanguage)) {
      const highlighted = hljs.highlight(sourceCode, {
        language: normalizedLanguage,
        ignoreIllegals: true,
      }).value
      return `<pre class="md-pre"><code class="md-code hljs${safeLanguageClass}">${highlighted}</code></pre>`
    }

    const highlighted = hljs.highlightAuto(sourceCode).value
    return `<pre class="md-pre"><code class="md-code hljs">${highlighted}</code></pre>`
  } catch {
    const escaped = escapeHtml(sourceCode)
    return `<pre class="md-pre"><code class="md-code${safeLanguageClass}">${escaped}</code></pre>`
  }
}

const markdownRenderer = new MarkdownIt({
  // 1. 禁止渲染原始 HTML，避免模型输出被直接注入页面。
  // 2. 保留换行语义，让对话消息中的软换行更接近聊天阅读习惯。
  // 3. 开启 linkify，自动识别纯文本链接，减少“写成网址却不可点”的情况。
  html: false,
  breaks: true,
  linkify: true,
  // 1. 统一在渲染阶段做代码高亮，避免在组件层重复处理字符串。
  // 2. 优先按模型返回的语言标记高亮，语言未知时自动推断。
  // 3. 高亮异常时自动降级为转义后的纯文本代码块，保证渲染不会中断。
  highlight(sourceCode: string, language: string) {
    return renderHighlightedCode(sourceCode, language)
  },
})

const defaultLinkOpenRenderer =
  markdownRenderer.renderer.rules.link_open ??
  ((tokens: any[], index: number, options: any, _env: any, self: any) =>
    self.renderToken(tokens, index, options))

markdownRenderer.renderer.rules.link_open = (
  tokens: any[],
  index: number,
  options: any,
  env: any,
  self: any,
) => {
  const token = tokens[index]

  // 1. 所有外链统一新窗口打开，避免覆盖当前对话页。
  // 2. 强制附加 rel，降低反向标签页劫持风险。
  token.attrSet('target', '_blank')
  token.attrSet('rel', 'noreferrer noopener')

  return defaultLinkOpenRenderer(tokens, index, options, env, self)
}

markdownRenderer.renderer.rules.table_open = () => '<div class="md-table-wrap"><table class="md-table">'
markdownRenderer.renderer.rules.table_close = () => '</table></div>'

// renderMarkdown 负责把聊天消息里的 Markdown 渲染为安全 HTML。
// 职责边界：
// 1. 负责常见 GFM 语法（包含表格、代码块）渲染，不负责业务字段裁剪与内容截断。
// 2. 负责输出可直接插入 v-html 的字符串，不负责 DOM 挂载与样式布局。
// 3. 若输入为空，仅返回空串，不抛异常阻断对话主链路。
export function renderMarkdown(input: string) {
  const normalized = (input || '').replace(/\r\n?/g, '\n').trim()
  if (!normalized) {
    return ''
  }

  return markdownRenderer.render(normalized)
}
