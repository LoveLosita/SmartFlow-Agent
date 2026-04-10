package rag

import (
	"log"

	"github.com/LoveLosita/smartflow/backend/infra/rag/core"
)

// ObserveLevel 对外暴露统一观测等级别名，避免启动层直接依赖 core 细节。
type ObserveLevel = core.ObserveLevel

const (
	ObserveLevelInfo  = core.ObserveLevelInfo
	ObserveLevelWarn  = core.ObserveLevelWarn
	ObserveLevelError = core.ObserveLevelError
)

// ObserveEvent 对外暴露统一观测事件别名。
type ObserveEvent = core.ObserveEvent

// Observer 对外暴露统一观测接口别名。
type Observer = core.Observer

// NewNopObserver 返回空实现。
func NewNopObserver() Observer {
	return core.NewNopObserver()
}

// NewLoggerObserver 返回标准日志适配器。
func NewLoggerObserver(logger *log.Logger) Observer {
	return core.NewLoggerObserver(logger)
}
