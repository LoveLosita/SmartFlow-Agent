package taskquery

import (
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/tool"
)

// buildInvokableToolMap 把工具包转换成“工具名 -> 可执行工具”映射。
//
// 职责边界：
// 1. 只做工具元数据到执行器的映射，不做业务逻辑；
// 2. 若工具包结构异常（数量不一致/信息缺失）直接返回 error；
// 3. 供图节点在运行时快速按工具名取执行器。
func buildInvokableToolMap(bundle *TaskQueryToolBundle) (map[string]tool.InvokableTool, error) {
	if bundle == nil || len(bundle.Tools) == 0 || len(bundle.ToolInfos) == 0 {
		return nil, fmt.Errorf("task query tool bundle is empty")
	}
	if len(bundle.Tools) != len(bundle.ToolInfos) {
		return nil, fmt.Errorf("task query tool bundle mismatch")
	}

	result := make(map[string]tool.InvokableTool, len(bundle.Tools))
	for idx, baseTool := range bundle.Tools {
		info := bundle.ToolInfos[idx]
		if info == nil || strings.TrimSpace(info.Name) == "" {
			return nil, fmt.Errorf("task query tool info is invalid")
		}
		invokableTool, ok := baseTool.(tool.InvokableTool)
		if !ok {
			return nil, fmt.Errorf("task query tool %s is not invokable", info.Name)
		}
		result[info.Name] = invokableTool
	}
	return result, nil
}
