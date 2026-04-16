package service

import (
	"time"

	infrarag "github.com/LoveLosita/smartflow/backend/infra/rag"
	memorymodel "github.com/LoveLosita/smartflow/backend/memory/model"
)

// buildReadScopedItemQuery 构造读侧统一使用的 MySQL 查询条件。
//
// 职责边界：
// 1. 只负责把 RetrieveRequest 映射成“读侧作用域”查询参数；
// 2. 不负责真正查库，也不负责排序、裁剪或注入；
// 3. conversation_id 字段在这里刻意不参与过滤，仅保留在记忆记录元数据里供审计与溯源使用。
//
// 步骤化说明：
// 1. 读侧始终按 user_id 作为硬隔离边界，避免跨用户串记忆。
// 2. assistant_id / run_id 仍允许参与过滤，因为它们表达的是助手实例与执行轮次边界，而不是“是否跨对话召回”的问题。
// 3. conversation_id 明确置空，原因是聊天上下文窗口已经覆盖同对话信息；记忆读侧的价值主要在跨对话补充。
func buildReadScopedItemQuery(
	req memorymodel.RetrieveRequest,
	now time.Time,
	statuses []string,
	limit int,
) memorymodel.ItemQuery {
	return memorymodel.ItemQuery{
		UserID:         req.UserID,
		ConversationID: "",
		AssistantID:    req.AssistantID,
		RunID:          req.RunID,
		Statuses:       statuses,
		MemoryTypes:    normalizeRetrieveMemoryTypes(req.MemoryTypes),
		IncludeGlobal:  true,
		OnlyUnexpired:  true,
		Limit:          limit,
		Now:            now,
	}
}

// buildReadScopedRAGRequest 构造读侧统一使用的 RAG 检索请求。
//
// 职责边界：
// 1. 只负责生成 memory 检索请求，不负责执行向量检索；
// 2. 不负责阈值外的重排、fallback 或去重；
// 3. conversation_id 字段同样只保留在文档 metadata 中，不再作为聊天读侧的硬过滤条件。
//
// 步骤化说明：
// 1. user_id 仍是唯一必须保留的硬过滤条件，确保召回范围限定在当前用户。
// 2. conversation_id 明确置空，避免旧对话记忆在进入相似度计算前就被 metadata filter 提前挡掉。
// 3. assistant_id / run_id 保持透传，方便后续若存在多助手场景时继续做更细粒度隔离。
func buildReadScopedRAGRequest(
	req memorymodel.RetrieveRequest,
	topK int,
	threshold float64,
) infrarag.MemoryRetrieveRequest {
	return infrarag.MemoryRetrieveRequest{
		Query:          req.Query,
		TopK:           topK,
		Threshold:      threshold,
		Action:         "search",
		UserID:         req.UserID,
		ConversationID: "",
		AssistantID:    req.AssistantID,
		RunID:          req.RunID,
		MemoryTypes:    normalizeRetrieveMemoryTypes(req.MemoryTypes),
	}
}

// shouldReturnSemanticRAGResult 判断当前是否可以直接采用 RAG 结果。
//
// 职责边界：
// 1. 只负责表达“RAG 是否足以短路后续 MySQL fallback”这一条业务规则；
// 2. 不负责执行任何检索，也不负责日志记录；
// 3. 返回 false 不代表错误，只代表调用方应继续尝试数据库兜底。
//
// 步骤化说明：
// 1. RAG 报错时，一定不能短路，必须继续走 MySQL fallback。
// 2. RAG 0 命中时，同样不能短路；否则会把“成功执行但没有候选”误当成最终结果。
// 3. 只有“无报错且结果非空”时，才允许直接返回 RAG 结果。
func shouldReturnSemanticRAGResult(items []memorymodel.ItemDTO, err error) bool {
	return err == nil && len(items) > 0
}
