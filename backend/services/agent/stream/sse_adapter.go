package agentstream

import "log"

// NewSSEPayloadEmitter 创建将 chunk 事件写入 outChan 的 emitter。
//
// 职责边界：
// 1. 接收 outChan（SSE 输出通道），返回 PayloadEmitter 函数；
// 2. 只把原始 JSON payload 写入通道，不添加 "data: " 前缀和 "\n\n" 后缀；
// 3. SSE 格式化（"data: " + payload + "\n\n"）由 API 层的 writeSSEData 统一处理；
// 4. 通道满时静默丢弃并返回 nil，让图继续完成状态持久化，避免因客户端超时而丢失快照。
//
// 使用示例：
//
//	emitter := NewSSEPayloadEmitter(outChan)
//	chunkEmitter := NewChunkEmitter(emitter, requestID, modelName, created)
//	chunkEmitter.EmitAssistantText("", "", "hello", true)
func NewSSEPayloadEmitter(outChan chan<- string) PayloadEmitter {
	return func(payload string) error {
		if outChan == nil {
			return nil
		}
		if payload == "" {
			return nil
		}
		select {
		case outChan <- payload:
			return nil
		default:
			// 通道已满：客户端可能已断开或消费过慢。
			// 静默丢弃此 chunk，让图继续执行并完成状态持久化。
			// 客户端重连后可从 Redis 快照恢复，不需要这条消息。
			log.Printf("[WARN] SSE outChan full, dropping payload (len=%d)", len(payload))
			return nil
		}
	}
}
