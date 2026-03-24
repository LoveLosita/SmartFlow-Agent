package agentnode

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// collectToolInfos 负责批量提取工具元信息，供模型注册与工具索引复用。
//
// 职责边界：
// 1. 只负责调用 tool.Info 并聚合返回结果。
// 2. 不负责校验工具是否可执行，也不负责按名称检索工具。
func collectToolInfos(ctx context.Context, tools []tool.BaseTool) ([]*schema.ToolInfo, error) {
	infos := make([]*schema.ToolInfo, 0, len(tools))
	for _, currentTool := range tools {
		info, err := currentTool.Info(ctx)
		if err != nil {
			return nil, fmt.Errorf("读取工具信息失败: %w", err)
		}
		infos = append(infos, info)
	}
	return infos, nil
}

// buildInvokableToolMap 负责把工具列表转换成“工具名 -> 可执行工具”的索引表。
//
// 步骤说明：
// 1. 先校验 tools 与 infos 是否一一对应，避免后续按下标取值时出现错配。
// 2. 再校验每个工具都带有合法名字，并且确实实现了 InvokableTool 接口。
// 3. 任一步失败都立即返回错误，避免 graph 在运行期拿到半残缺的工具集合。
func buildInvokableToolMap(tools []tool.BaseTool, infos []*schema.ToolInfo) (map[string]tool.InvokableTool, error) {
	if len(tools) == 0 || len(infos) == 0 {
		return nil, errors.New("tool bundle is empty")
	}
	if len(tools) != len(infos) {
		return nil, errors.New("tool bundle mismatch")
	}

	result := make(map[string]tool.InvokableTool, len(tools))
	for idx, currentTool := range tools {
		info := infos[idx]
		if info == nil || strings.TrimSpace(info.Name) == "" {
			return nil, errors.New("tool info is invalid")
		}
		invokable, ok := currentTool.(tool.InvokableTool)
		if !ok {
			return nil, fmt.Errorf("tool %s is not invokable", info.Name)
		}
		result[info.Name] = invokable
	}
	return result, nil
}

// getInvokableToolByName 负责从工具集合中提取指定名称的可执行工具。
//
// 职责边界：
// 1. 负责复用统一索引逻辑，避免各业务链路重复写名称查找代码。
// 2. 不负责兜底选择其他工具；未命中时直接返回错误，由上层决定如何处理。
func getInvokableToolByName(tools []tool.BaseTool, infos []*schema.ToolInfo, name string) (tool.InvokableTool, error) {
	invokableMap, err := buildInvokableToolMap(tools, infos)
	if err != nil {
		return nil, err
	}
	invokable, ok := invokableMap[name]
	if !ok {
		return nil, fmt.Errorf("tool %s not found", name)
	}
	return invokable, nil
}
