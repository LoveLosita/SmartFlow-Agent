package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	activeapply "github.com/LoveLosita/smartflow/backend/active_scheduler/apply"
	activepreview "github.com/LoveLosita/smartflow/backend/active_scheduler/preview"
	activesvc "github.com/LoveLosita/smartflow/backend/active_scheduler/service"
	"github.com/LoveLosita/smartflow/backend/active_scheduler/trigger"
	"github.com/LoveLosita/smartflow/backend/respond"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ActiveScheduleAPI 承载主动调度开发期和验收期 API。
//
// 职责边界：
// 1. 只负责鉴权用户、绑定请求和调用主动调度 service；
// 2. 不直接读取 DAO、不生成候选、不写 preview；
// 3. 阶段 1-2 只开放 dry-run，正式 trigger/preview/confirm 后续阶段再接入。
type ActiveScheduleAPI struct {
	dryRunService         *activesvc.DryRunService
	previewConfirmService *activesvc.PreviewConfirmService
	triggerService        *activesvc.TriggerService
}

func NewActiveScheduleAPI(dryRunService *activesvc.DryRunService, previewConfirmService *activesvc.PreviewConfirmService, triggerService *activesvc.TriggerService) *ActiveScheduleAPI {
	return &ActiveScheduleAPI{
		dryRunService:         dryRunService,
		previewConfirmService: previewConfirmService,
		triggerService:        triggerService,
	}
}

type ActiveScheduleDryRunRequest struct {
	TriggerType    string     `json:"trigger_type" binding:"required"`
	TargetType     string     `json:"target_type" binding:"required"`
	TargetID       int        `json:"target_id"`
	FeedbackID     string     `json:"feedback_id"`
	IdempotencyKey string     `json:"idempotency_key"`
	MockNow        *time.Time `json:"mock_now"`
	Payload        any        `json:"payload"`
}

// DryRun 同步执行主动调度诊断，不写 preview、不发通知、不修改正式日程。
func (api *ActiveScheduleAPI) DryRun(c *gin.Context) {
	if api == nil || api.dryRunService == nil {
		c.JSON(http.StatusInternalServerError, respond.InternalError(nilServiceError("主动调度 dry-run service 未初始化")))
		return
	}

	var req ActiveScheduleDryRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}

	userID := c.GetInt("user_id")
	now := time.Now()
	isMockTime := req.MockNow != nil
	trig := trigger.ActiveScheduleTrigger{
		UserID:         userID,
		TriggerType:    trigger.TriggerType(req.TriggerType),
		Source:         trigger.SourceAPIDryRun,
		TargetType:     trigger.TargetType(req.TargetType),
		TargetID:       req.TargetID,
		FeedbackID:     req.FeedbackID,
		IdempotencyKey: req.IdempotencyKey,
		MockNow:        req.MockNow,
		IsMockTime:     isMockTime,
		RequestedAt:    now,
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()
	result, err := api.dryRunService.DryRun(ctx, trig)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}

	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, result))
}

// Trigger 写入正式主动调度 trigger 并发布 active_schedule.triggered。
func (api *ActiveScheduleAPI) Trigger(c *gin.Context) {
	if api == nil || api.triggerService == nil {
		c.JSON(http.StatusInternalServerError, respond.InternalError(nilServiceError("主动调度 trigger service 未初始化")))
		return
	}

	var req ActiveScheduleDryRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	rawPayload, err := json.Marshal(req.Payload)
	if err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	if string(rawPayload) == "null" {
		rawPayload = []byte("{}")
	}

	now := time.Now()
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()
	result, err := api.triggerService.CreateAndPublish(ctx, activesvc.TriggerRequest{
		UserID:         c.GetInt("user_id"),
		TriggerType:    trigger.TriggerType(req.TriggerType),
		Source:         trigger.SourceAPITrigger,
		TargetType:     trigger.TargetType(req.TargetType),
		TargetID:       req.TargetID,
		FeedbackID:     req.FeedbackID,
		IdempotencyKey: req.IdempotencyKey,
		MockNow:        req.MockNow,
		IsMockTime:     req.MockNow != nil,
		RequestedAt:    now,
		Payload:        rawPayload,
		TraceID:        fmt.Sprintf("trace_api_trigger_%d_%d", c.GetInt("user_id"), now.UnixNano()),
	})
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, result))
}

// CreatePreview 先同步 dry-run，再把 top1 候选固化为待确认预览。
func (api *ActiveScheduleAPI) CreatePreview(c *gin.Context) {
	if api == nil || api.dryRunService == nil || api.previewConfirmService == nil {
		c.JSON(http.StatusInternalServerError, respond.InternalError(nilServiceError("主动调度 preview service 未初始化")))
		return
	}

	var req ActiveScheduleDryRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}

	userID := c.GetInt("user_id")
	now := time.Now()
	trig := trigger.ActiveScheduleTrigger{
		TriggerID:      fmt.Sprintf("ast_api_%d_%d", userID, now.UnixNano()),
		UserID:         userID,
		TriggerType:    trigger.TriggerType(req.TriggerType),
		Source:         trigger.SourceAPIDryRun,
		TargetType:     trigger.TargetType(req.TargetType),
		TargetID:       req.TargetID,
		FeedbackID:     req.FeedbackID,
		IdempotencyKey: req.IdempotencyKey,
		MockNow:        req.MockNow,
		IsMockTime:     req.MockNow != nil,
		RequestedAt:    now,
		TraceID:        fmt.Sprintf("trace_api_preview_%d_%d", userID, now.UnixNano()),
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	dryRunResult, err := api.dryRunService.DryRun(ctx, trig)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	previewResp, err := api.previewConfirmService.CreatePreviewFromDryRun(ctx, activepreview.CreatePreviewRequest{
		ActiveContext: dryRunResult.Context,
		Observation:   dryRunResult.Observation,
		Candidates:    dryRunResult.Candidates,
		TriggerID:     trig.TriggerID,
		GeneratedAt:   now,
	})
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, previewResp.Detail))
}

// GetPreview 查询主动调度预览详情。
func (api *ActiveScheduleAPI) GetPreview(c *gin.Context) {
	if api == nil || api.previewConfirmService == nil {
		c.JSON(http.StatusInternalServerError, respond.InternalError(nilServiceError("主动调度 preview service 未初始化")))
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()
	detail, err := api.previewConfirmService.GetPreview(ctx, c.GetInt("user_id"), c.Param("preview_id"))
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, detail))
}

// ConfirmPreview 同步确认并正式应用主动调度预览。
func (api *ActiveScheduleAPI) ConfirmPreview(c *gin.Context) {
	if api == nil || api.previewConfirmService == nil {
		c.JSON(http.StatusInternalServerError, respond.InternalError(nilServiceError("主动调度 confirm service 未初始化")))
		return
	}
	var req activeapply.ConfirmRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	req.PreviewID = c.Param("preview_id")
	req.UserID = c.GetInt("user_id")
	if req.RequestedAt.IsZero() {
		req.RequestedAt = time.Now()
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	result, err := api.previewConfirmService.ConfirmPreview(ctx, req)
	if err != nil {
		writeActiveScheduleConfirmError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, result))
}

// writeActiveScheduleConfirmError 将 confirm/apply 的可预期业务拒绝映射为 4xx。
//
// 职责边界：
// 1. 只处理主动调度 confirm/apply 链路已经分类的 ApplyError；
// 2. 不吞掉数据库、超时、panic recover 等系统错误，未知错误继续交给通用 respond 走 500；
// 3. 响应体保留 error_code / error_message，便于前端按过期、冲突、越权等场景给出明确交互。
func writeActiveScheduleConfirmError(c *gin.Context, err error) {
	if applyErr, ok := activeapply.AsApplyError(err); ok {
		status := activeScheduleApplyHTTPStatus(applyErr.Code)
		message := applyErr.Message
		if message == "" {
			message = applyErr.Error()
		}
		applyStatus := activeapply.ApplyStatusRejected
		if applyErr.Code == activeapply.ErrorCodeExpired {
			applyStatus = activeapply.ApplyStatusExpired
		}
		if applyErr.Code == activeapply.ErrorCodeDBError {
			applyStatus = activeapply.ApplyStatusFailed
		}
		c.JSON(status, respond.RespWithData(respond.Response{
			Status: fmt.Sprintf("%d", status),
			Info:   message,
		}, activeapply.ConfirmResult{
			ApplyStatus:  applyStatus,
			ErrorCode:    applyErr.Code,
			ErrorMessage: message,
		}))
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, respond.RespWithData(respond.Response{
			Status: fmt.Sprintf("%d", http.StatusNotFound),
			Info:   "预览不存在或已被删除",
		}, activeapply.ConfirmResult{
			ApplyStatus:  activeapply.ApplyStatusRejected,
			ErrorCode:    activeapply.ErrorCodeTargetNotFound,
			ErrorMessage: "预览不存在或已被删除",
		}))
		return
	}
	respond.DealWithError(c, err)
}

// activeScheduleApplyHTTPStatus 只负责错误码到 HTTP 语义的稳定映射。
//
// 说明：
// 1. 请求体/编辑范围问题返回 400；
// 2. 越权返回 403，目标缺失返回 404；
// 3. 过期、幂等冲突、节次冲突、目标状态变化统一返回 409，提示前端刷新预览或重新生成。
func activeScheduleApplyHTTPStatus(code activeapply.ErrorCode) int {
	switch code {
	case activeapply.ErrorCodeInvalidRequest,
		activeapply.ErrorCodeInvalidEditedChanges,
		activeapply.ErrorCodeUnsupportedChangeType:
		return http.StatusBadRequest
	case activeapply.ErrorCodeForbidden:
		return http.StatusForbidden
	case activeapply.ErrorCodeTargetNotFound:
		return http.StatusNotFound
	case activeapply.ErrorCodeExpired,
		activeapply.ErrorCodeIdempotencyConflict,
		activeapply.ErrorCodeBaseVersionChanged,
		activeapply.ErrorCodeTargetCompleted,
		activeapply.ErrorCodeTargetAlreadySchedule,
		activeapply.ErrorCodeSlotConflict,
		activeapply.ErrorCodeAlreadyApplied:
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

type nilServiceError string

func (e nilServiceError) Error() string {
	return string(e)
}
