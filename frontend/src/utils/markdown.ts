function escapeHtml(input: string) {
  return input
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#39;')
}

function parseInlineMarkdown(input: string) {
  const inlineCodeBlocks: string[] = []
  let content = escapeHtml(input)

  content = content.replace(/`([^`]+)`/g, (_, code: string) => {
    const token = `@@INLINE_CODE_${inlineCodeBlocks.length}@@`
    inlineCodeBlocks.push(`<code>${escapeHtml(code)}</code>`)
    return token
  })

  content = content.replace(
    /\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)/g,
    (_, label: string, link: string) =>
      `<a href="${escapeHtml(link)}" target="_blank" rel="noreferrer noopener">${label}</a>`,
  )

  content = content.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
  content = content.replace(/\*([^*]+)\*/g, '<em>$1</em>')
  content = content.replace(/~~([^~]+)~~/g, '<del>$1</del>')

  return content.replace(/@@INLINE_CODE_(\d+)@@/g, (_, index: string) => inlineCodeBlocks[Number(index)] ?? '')
}

// renderMarkdown 负责把常见 Markdown 文本安全转换为可展示 HTML。
// 职责边界：
// 1. 负责处理标题、列表、引用、代码块、链接、粗斜体等常见场景。
// 2. 不追求完整 CommonMark 兼容，只覆盖聊天消息里最常见的展示需求。
// 3. 所有原始文本都会先做 HTML 转义，避免把模型输出直接当成原生 HTML 注入页面。
export function renderMarkdown(input: string) {
  const normalized = (input || '').replace(/\r\n?/g, '\n').trim()
  if (!normalized) {
    return ''
  }

  const fencedBlocks: string[] = []
  let source = normalized.replace(/```([a-zA-Z0-9_-]+)?\n?([\s\S]*?)```/g, (_, language: string, code: string) => {
    const token = `@@FENCED_BLOCK_${fencedBlocks.length}@@`
    const languageClass = language ? ` language-${escapeHtml(language)}` : ''
    fencedBlocks.push(
      `<pre class="md-pre"><code class="md-code${languageClass}">${escapeHtml(code.trimEnd())}</code></pre>`,
    )
    return token
  })

  const lines = source.split('\n')
  const htmlParts: string[] = []
  let unorderedItems: string[] = []
  let orderedItems: string[] = []
  let quoteLines: string[] = []
  let paragraphLines: string[] = []

  function flushParagraph() {
    if (paragraphLines.length === 0) {
      return
    }
    htmlParts.push(`<p>${parseInlineMarkdown(paragraphLines.join('<br />'))}</p>`)
    paragraphLines = []
  }

  function flushUnorderedList() {
    if (unorderedItems.length === 0) {
      return
    }
    htmlParts.push(`<ul>${unorderedItems.map((item) => `<li>${parseInlineMarkdown(item)}</li>`).join('')}</ul>`)
    unorderedItems = []
  }

  function flushOrderedList() {
    if (orderedItems.length === 0) {
      return
    }
    htmlParts.push(`<ol>${orderedItems.map((item) => `<li>${parseInlineMarkdown(item)}</li>`).join('')}</ol>`)
    orderedItems = []
  }

  function flushBlockquote() {
    if (quoteLines.length === 0) {
      return
    }
    htmlParts.push(`<blockquote>${quoteLines.map((line) => `<p>${parseInlineMarkdown(line)}</p>`).join('')}</blockquote>`)
    quoteLines = []
  }

  function flushAllBlocks() {
    flushParagraph()
    flushUnorderedList()
    flushOrderedList()
    flushBlockquote()
  }

  for (const line of lines) {
    const trimmed = line.trim()

    if (!trimmed) {
      flushAllBlocks()
      continue
    }

    const headingMatch = trimmed.match(/^(#{1,6})\s+(.*)$/)
    if (headingMatch) {
      flushAllBlocks()
      const level = headingMatch[1].length
      htmlParts.push(`<h${level}>${parseInlineMarkdown(headingMatch[2])}</h${level}>`)
      continue
    }

    const unorderedMatch = trimmed.match(/^[-*+]\s+(.*)$/)
    if (unorderedMatch) {
      flushParagraph()
      flushOrderedList()
      flushBlockquote()
      unorderedItems.push(unorderedMatch[1])
      continue
    }

    const orderedMatch = trimmed.match(/^\d+\.\s+(.*)$/)
    if (orderedMatch) {
      flushParagraph()
      flushUnorderedList()
      flushBlockquote()
      orderedItems.push(orderedMatch[1])
      continue
    }

    const quoteMatch = trimmed.match(/^>\s?(.*)$/)
    if (quoteMatch) {
      flushParagraph()
      flushUnorderedList()
      flushOrderedList()
      quoteLines.push(quoteMatch[1])
      continue
    }

    paragraphLines.push(trimmed)
  }

  flushAllBlocks()

  return htmlParts
    .join('')
    .replace(/@@FENCED_BLOCK_(\d+)@@/g, (_, index: string) => fencedBlocks[Number(index)] ?? '')
}
