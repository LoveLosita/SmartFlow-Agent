// Package respond 承载 gateway HTTP 门面使用的响应适配入口。
//
// 职责边界：
// 1. 只面向 gateway/api 与 gateway/middleware，统一 HTTP JSON 写回与错误响应常量的 import 位置；
// 2. 迁移期继续复用根 backend/respond 的响应码和错误语义，避免一次性改动服务层、RPC 层和 client 层；
// 3. 不承载任何服务私有业务逻辑，服务代码禁止反向 import backend/gateway/shared/respond。
package respond

import (
	"errors"
	"net/http"

	rootrespond "github.com/LoveLosita/smartflow/backend/shared/respond"
	"github.com/gin-gonic/gin"
)

type (
	// Response 是 gateway 透传给前端的项目响应码结构。
	Response = rootrespond.Response

	// FinalResponse 是带 data 字段的统一 HTTP 响应结构。
	FinalResponse = rootrespond.FinalResponse
)

var (
	Ok                          = rootrespond.Ok
	UserTasksEmpty              = rootrespond.UserTasksEmpty
	NoOngoingOrUpcomingSchedule = rootrespond.NoOngoingOrUpcomingSchedule
	TaskAlreadyDeleted          = rootrespond.TaskAlreadyDeleted
	WrongParamType              = rootrespond.WrongParamType
	MissingParam                = rootrespond.MissingParam
	MissingIdempotencyKey       = rootrespond.MissingIdempotencyKey
	MissingToken                = rootrespond.MissingToken
	InvalidClaims               = rootrespond.InvalidClaims
	ErrUnauthorized             = rootrespond.ErrUnauthorized
	RequestIsProcessing         = rootrespond.RequestIsProcessing
	ScheduleConflict            = rootrespond.ScheduleConflict
	TooManyRequests             = rootrespond.TooManyRequests
	TokenUsageExceedsLimit      = rootrespond.TokenUsageExceedsLimit
	ConversationNotFound        = rootrespond.ConversationNotFound
	MissingConversationID       = rootrespond.MissingConversationID
)

// RespWithData 为 gateway HTTP 门面生成带 data 的统一响应体。
//
// 职责边界：
// 1. 只做响应结构组装，不决定 HTTP 状态码；
// 2. 响应码来源仍是根 respond，保证迁移前后前端协议不变。
func RespWithData(response Response, data interface{}) FinalResponse {
	return rootrespond.RespWithData(response, data)
}

// DealWithError 将项目 error 映射为 HTTP JSON 响应。
//
// 职责边界：
// 1. 只在 gateway HTTP 层写响应；
// 2. 业务错误语义仍由根 respond 统一维护；
// 3. nil error 直接忽略，保持旧 DealWithError 的降级语义。
func DealWithError(c *gin.Context, err error) {
	if err == nil {
		return
	}
	var resp Response
	if errors.Is(err, UserTasksEmpty) || errors.Is(err, NoOngoingOrUpcomingSchedule) || errors.Is(err, TaskAlreadyDeleted) {
		c.JSON(http.StatusOK, err)
		return
	}
	if errors.As(err, &resp) {
		c.JSON(resp.HTTPStatus(), resp)
		return
	}
	c.JSON(http.StatusInternalServerError, InternalError(err))
}

// InternalError 生成 500 类响应体，供 gateway 依赖缺失等边缘错误使用。
func InternalError(err error) Response {
	return rootrespond.InternalError(err)
}
