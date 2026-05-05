package sv

import (
	"fmt"
	"strings"

	memorymodel "github.com/LoveLosita/smartflow/backend/memory/model"
)

// renderMemoryPinnedContentByMode 根据配置选择记忆渲染方式。
func renderMemoryPinnedContentByMode(items []memorymodel.ItemDTO, renderMode string) string {
	switch memorymodel.NormalizeInjectRenderMode(renderMode) {
	case memorymodel.MemoryInjectRenderModeTypedV2:
		return RenderTypedMemoryContent(items)
	default:
		return RenderFlatMemoryContent(items)
	}
}

// RenderFlatMemoryContent 生成兼容旧链路的扁平记忆文本。
func RenderFlatMemoryContent(items []memorymodel.ItemDTO) string {
	if len(items) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(agentMemoryIntroLine)

	seen := make(map[string]struct{}, len(items))
	written := 0
	for _, item := range items {
		line := buildMemoryPinnedLine(item)
		if line == "" {
			continue
		}
		if _, exists := seen[line]; exists {
			continue
		}
		seen[line] = struct{}{}
		sb.WriteString("\n- ")
		sb.WriteString(line)
		written++
	}

	if written == 0 {
		return ""
	}
	return strings.TrimSpace(sb.String())
}

// RenderTypedMemoryContent 按记忆类型分段渲染。
//
// 步骤化说明：
// 1. 先按固定类型顺序分组，避免同类记忆在 prompt 中被打散；
// 2. 每组内部继续做文本级去重，兜底保护历史脏数据；
// 3. 只输出非空分组，减少 Execute / Plan 阶段的无效噪音。
func RenderTypedMemoryContent(items []memorymodel.ItemDTO) string {
	if len(items) == 0 {
		return ""
	}

	type renderSection struct {
		Title string
		Items []string
	}
	orderedTypes := []string{
		memorymodel.MemoryTypeConstraint,
		memorymodel.MemoryTypePreference,
		memorymodel.MemoryTypeFact,
	}
	sectionTitle := map[string]string{
		memorymodel.MemoryTypeConstraint: "必守约束",
		memorymodel.MemoryTypePreference: "用户偏好",
		memorymodel.MemoryTypeFact:       "当前话题相关事实",
	}

	grouped := make(map[string][]string, len(orderedTypes))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		content := buildMemoryRenderContent(item)
		if content == "" {
			continue
		}
		dedupKey := strings.TrimSpace(item.MemoryType) + "::" + content
		if _, exists := seen[dedupKey]; exists {
			continue
		}
		seen[dedupKey] = struct{}{}

		memoryType := memorymodel.NormalizeMemoryType(item.MemoryType)
		if memoryType == "" {
			memoryType = memorymodel.MemoryTypeFact
		}
		grouped[memoryType] = append(grouped[memoryType], content)
	}

	sections := make([]renderSection, 0, len(orderedTypes))
	for _, memoryType := range orderedTypes {
		contentList := grouped[memoryType]
		if len(contentList) == 0 {
			continue
		}
		sections = append(sections, renderSection{
			Title: sectionTitle[memoryType],
			Items: contentList,
		})
	}
	if len(sections) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(agentMemoryIntroLine)
	for _, section := range sections {
		sb.WriteString("\n\n【")
		sb.WriteString(section.Title)
		sb.WriteString("】")
		for _, line := range section.Items {
			sb.WriteString("\n- ")
			sb.WriteString(line)
		}
	}
	return strings.TrimSpace(sb.String())
}

// buildMemoryPinnedLine 把单条记忆渲染成“[类型] 内容”的简洁格式。
func buildMemoryPinnedLine(item memorymodel.ItemDTO) string {
	text := buildMemoryRenderContent(item)
	if text == "" {
		return ""
	}
	return fmt.Sprintf("[%s] %s", localizeMemoryType(item.MemoryType), text)
}

func buildMemoryRenderContent(item memorymodel.ItemDTO) string {
	text := strings.TrimSpace(item.Content)
	if text == "" {
		text = strings.TrimSpace(item.Title)
	}
	return text
}

// localizeMemoryType 把 memory 类型映射成 prompt 里更自然的中文标签。
func localizeMemoryType(memoryType string) string {
	switch strings.TrimSpace(memoryType) {
	case memorymodel.MemoryTypePreference:
		return "偏好"
	case memorymodel.MemoryTypeConstraint:
		return "约束"
	case memorymodel.MemoryTypeFact:
		return "事实"
	default:
		return "记忆"
	}
}
