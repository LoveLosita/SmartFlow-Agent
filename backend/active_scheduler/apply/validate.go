package apply

import (
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/model"
)

// IsPreviewExpired 判断 preview 是否已经超过确认有效期。
//
// 职责边界：
// 1. 只比较 expires_at 与调用方传入的 now；
// 2. 不读取数据库，也不更新 preview.status；
// 3. now 为空时按“不能安全确认”处理，避免调用方误放过过期预览。
func IsPreviewExpired(preview model.ActiveSchedulePreview, now time.Time) bool {
	if now.IsZero() || preview.ExpiresAt.IsZero() {
		return true
	}
	return !now.Before(preview.ExpiresAt)
}

// IsPreviewOwnedByUser 判断 preview 是否归属当前用户。
//
// 职责边界：
// 1. 只做 user_id 等值判断；
// 2. userID 非法时直接返回 false；
// 3. 不判断用户是否仍存在，该事实应由 API 鉴权或接入层保证。
func IsPreviewOwnedByUser(preview model.ActiveSchedulePreview, userID int) bool {
	return userID > 0 && preview.UserID == userID
}

// IsPreviewAlreadyApplied 判断 preview 是否已经成功应用过。
//
// 职责边界：
// 1. 同时兼容 preview.status 与 apply_status 两个字段；
// 2. 只识别“已成功应用”，不把 failed/rejected 视为成功；
// 3. 返回 true 时主线程应避免再次写正式日程。
func IsPreviewAlreadyApplied(preview model.ActiveSchedulePreview) bool {
	return preview.Status == model.ActiveSchedulePreviewStatusApplied ||
		preview.ApplyStatus == model.ActiveScheduleApplyStatusApplied
}

// ValidatePreviewConfirmable 执行 confirm 入口的基础 preview 判断。
//
// 职责边界：
// 1. 只校验预览归属、过期、状态与已应用等轻量规则；
// 2. 不校验 task/schedule 当前真值，也不判断冲突，正式重校验由 apply port 完成；
// 3. 返回 nil 表示可以继续做候选转换，返回 ApplyError 表示本次 confirm 应被拒绝。
func ValidatePreviewConfirmable(preview model.ActiveSchedulePreview, userID int, now time.Time) error {
	if preview.ID == "" {
		return newApplyError(ErrorCodeTargetNotFound, "预览不存在或未加载", nil)
	}
	if !IsPreviewOwnedByUser(preview, userID) {
		return newApplyError(ErrorCodeForbidden, "预览不属于当前用户", nil)
	}
	if IsPreviewExpired(preview, now) || preview.Status == model.ActiveSchedulePreviewStatusExpired || preview.ApplyStatus == model.ActiveScheduleApplyStatusExpired {
		return newApplyError(ErrorCodeExpired, "预览已过期，请重新生成建议", nil)
	}
	if IsPreviewAlreadyApplied(preview) {
		return newApplyError(ErrorCodeAlreadyApplied, "该预览已经应用过，不能重复写入日程", nil)
	}
	if preview.Status == model.ActiveSchedulePreviewStatusIgnored {
		return newApplyError(ErrorCodeInvalidRequest, "该预览已被忽略，不能继续确认", nil)
	}
	if preview.Status == model.ActiveSchedulePreviewStatusFailed {
		return newApplyError(ErrorCodeInvalidRequest, "该预览生成失败，不能继续确认", nil)
	}
	if preview.Status != "" && preview.Status != model.ActiveSchedulePreviewStatusReady && preview.Status != model.ActiveSchedulePreviewStatusPending {
		return newApplyError(ErrorCodeInvalidRequest, "预览状态不允许确认", nil)
	}
	if preview.ApplyStatus != "" &&
		preview.ApplyStatus != model.ActiveScheduleApplyStatusNone &&
		preview.ApplyStatus != model.ActiveScheduleApplyStatusFailed &&
		preview.ApplyStatus != model.ActiveScheduleApplyStatusRejected {
		return newApplyError(ErrorCodeInvalidRequest, "当前 apply 状态不允许重新确认", nil)
	}
	return nil
}

// DetectIdempotencyConflict 判断同一个 preview_id + idempotency_key 是否被复用于不同请求体。
//
// 职责边界：
// 1. 只比较当前请求和 preview 已记录的 apply_idempotency_key / apply_request_hash；
// 2. 不查询数据库唯一约束，主线程仍需要在事务或行锁内调用；
// 3. 返回 true 表示必须拒绝，避免同 key 不同内容污染审计链路。
func DetectIdempotencyConflict(preview model.ActiveSchedulePreview, requestHash string, idempotencyKey string) bool {
	if strings.TrimSpace(idempotencyKey) == "" || strings.TrimSpace(preview.ApplyIdempotencyKey) == "" {
		return false
	}
	if preview.ApplyIdempotencyKey != idempotencyKey {
		return false
	}
	if preview.ApplyRequestHash == "" {
		return false
	}
	return preview.ApplyRequestHash != requestHash
}
