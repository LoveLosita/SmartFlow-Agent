package newagentstream

import (
	"fmt"
)

// NewSSEPayloadEmitter 创建将 chunk 事件写入 outChan 的 emitter。
//
// 职责边界：
// 1. 接收 outChan（SSE 输出通道），返回 PayloadEmitter 函数；
// 2. 只把原始 JSON payload 写入通道，不添加 "data: " 前缀和 "\n\n" 后缀；
// 3. SSE 格式化（"data: " + payload + "\n\n"）由 API 层的 writeSSEData 统一处理；
// 4. 发送失败时返回 error，但不关闭通道（通道由调用方管理）。
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
			// 通道已满或已关闭：不阻塞，直接返回错误。
			return fmt.Errorf("outChan full or closed")
		}
	}
}
