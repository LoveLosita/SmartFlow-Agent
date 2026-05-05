package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/LoveLosita/smartflow/backend/gateway/shared/respond"
	contracts "github.com/LoveLosita/smartflow/backend/shared/contracts/activescheduler"
	"github.com/LoveLosita/smartflow/backend/shared/ports"
	"github.com/gin-gonic/gin"
)

const activeScheduleAPITimeout = 8 * time.Second

// ActiveScheduleAPI 承载主动调度开发期和验收期 API。
//
// 职责边界：
// 1. 只负责鉴权用户、绑定请求和调用 active-scheduler zrpc client；
// 2. 不直接读取 DAO、不生成候选、不写 preview；
// 3. 复杂响应由 active-scheduler 服务返回 JSON，gateway 只做边缘透传。
type ActiveScheduleAPI struct {
	client ports.ActiveSchedulerCommandClient
}

func NewActiveScheduleAPI(client ports.ActiveSchedulerCommandClient) *ActiveScheduleAPI {
	return &ActiveScheduleAPI{client: client}
}

// DryRun 同步执行主动调度诊断，不写 preview、不发通知、不修改正式日程。
func (api *ActiveScheduleAPI) DryRun(c *gin.Context) {
	if api == nil || api.client == nil {
		c.JSON(http.StatusInternalServerError, respond.InternalError(nilServiceError("主动调度 zrpc client 未初始化")))
		return
	}

	var req contracts.ActiveScheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	req.UserID = c.GetInt("user_id")

	ctx, cancel := context.WithTimeout(c.Request.Context(), activeScheduleAPITimeout)
	defer cancel()
	result, err := api.client.DryRun(ctx, req)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, result))
}

// Trigger 写入正式主动调度 trigger 并发布 active_schedule.triggered。
func (api *ActiveScheduleAPI) Trigger(c *gin.Context) {
	if api == nil || api.client == nil {
		c.JSON(http.StatusInternalServerError, respond.InternalError(nilServiceError("主动调度 zrpc client 未初始化")))
		return
	}

	var req contracts.ActiveScheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	req.UserID = c.GetInt("user_id")

	ctx, cancel := context.WithTimeout(c.Request.Context(), activeScheduleAPITimeout)
	defer cancel()
	result, err := api.client.Trigger(ctx, req)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, result))
}

// CreatePreview 先同步 dry-run，再把 top1 候选固化为待确认预览。
func (api *ActiveScheduleAPI) CreatePreview(c *gin.Context) {
	if api == nil || api.client == nil {
		c.JSON(http.StatusInternalServerError, respond.InternalError(nilServiceError("主动调度 zrpc client 未初始化")))
		return
	}

	var req contracts.ActiveScheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	req.UserID = c.GetInt("user_id")

	ctx, cancel := context.WithTimeout(c.Request.Context(), activeScheduleAPITimeout)
	defer cancel()
	result, err := api.client.CreatePreview(ctx, req)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, result))
}

// GetPreview 查询主动调度预览详情。
func (api *ActiveScheduleAPI) GetPreview(c *gin.Context) {
	if api == nil || api.client == nil {
		c.JSON(http.StatusInternalServerError, respond.InternalError(nilServiceError("主动调度 zrpc client 未初始化")))
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), activeScheduleAPITimeout)
	defer cancel()
	detail, err := api.client.GetPreview(ctx, contracts.GetPreviewRequest{
		UserID:    c.GetInt("user_id"),
		PreviewID: c.Param("preview_id"),
	})
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, detail))
}

// ConfirmPreview 同步确认并正式应用主动调度预览。
func (api *ActiveScheduleAPI) ConfirmPreview(c *gin.Context) {
	if api == nil || api.client == nil {
		c.JSON(http.StatusInternalServerError, respond.InternalError(nilServiceError("主动调度 zrpc client 未初始化")))
		return
	}
	var req contracts.ConfirmPreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	req.PreviewID = c.Param("preview_id")
	req.UserID = c.GetInt("user_id")
	if req.RequestedAt.IsZero() {
		req.RequestedAt = time.Now()
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), activeScheduleAPITimeout)
	defer cancel()
	result, err := api.client.ConfirmPreview(ctx, req)
	if err != nil {
		writeActiveScheduleConfirmError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, result))
}

// writeActiveScheduleConfirmError 将 confirm/apply 的可预期业务拒绝映射为 4xx。
//
// 职责边界：
// 1. 只处理 active-scheduler zrpc client 已恢复的 ApplyError；
// 2. 不吞掉数据库、超时、panic recover 等系统错误，未知错误继续交给通用 respond 走 500；
// 3. 响应体保留 error_code / error_message，便于前端按过期、冲突、越权等场景给出明确交互。
func writeActiveScheduleConfirmError(c *gin.Context, err error) {
	var applyErr *contracts.ApplyError
	if errors.As(err, &applyErr) {
		status := activeScheduleApplyHTTPStatus(applyErr.Code)
		message := applyErr.Message
		if message == "" {
			message = applyErr.Error()
		}
		applyStatus := contracts.ApplyStatusRejected
		if applyErr.Code == contracts.ApplyErrorCodeExpired {
			applyStatus = contracts.ApplyStatusExpired
		}
		if applyErr.Code == contracts.ApplyErrorCodeDBError {
			applyStatus = contracts.ApplyStatusFailed
		}
		c.JSON(status, respond.RespWithData(respond.Response{
			Status: fmt.Sprintf("%d", status),
			Info:   message,
		}, contracts.ConfirmErrorResult{
			ApplyStatus:  applyStatus,
			ErrorCode:    applyErr.Code,
			ErrorMessage: message,
		}))
		return
	}
	respond.DealWithError(c, err)
}

// activeScheduleApplyHTTPStatus 只负责错误码到 HTTP 语义的稳定映射。
func activeScheduleApplyHTTPStatus(code contracts.ApplyErrorCode) int {
	switch code {
	case contracts.ApplyErrorCodeInvalidRequest,
		contracts.ApplyErrorCodeInvalidEditedChanges,
		contracts.ApplyErrorCodeUnsupportedChangeType:
		return http.StatusBadRequest
	case contracts.ApplyErrorCodeForbidden:
		return http.StatusForbidden
	case contracts.ApplyErrorCodeTargetNotFound:
		return http.StatusNotFound
	case contracts.ApplyErrorCodeExpired,
		contracts.ApplyErrorCodeIdempotencyConflict,
		contracts.ApplyErrorCodeBaseVersionChanged,
		contracts.ApplyErrorCodeTargetCompleted,
		contracts.ApplyErrorCodeTargetAlreadySchedule,
		contracts.ApplyErrorCodeSlotConflict,
		contracts.ApplyErrorCodeAlreadyApplied:
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

type nilServiceError string

func (e nilServiceError) Error() string {
	return string(e)
}
