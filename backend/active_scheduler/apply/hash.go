package apply

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

type RequestHash struct {
	BodyHash  string
	ApplyHash string
	ApplyID   string
}

// BuildConfirmRequestHash 计算 confirm 请求的幂等摘要。
//
// 职责边界：
// 1. body_hash 只覆盖一次确认动作中真正影响 apply 的 body 字段；
// 2. apply_hash 按 preview_id + idempotency_key + body_hash 计算，满足同 key 不同内容可识别；
// 3. 不查询历史记录，是否冲突由 DetectIdempotencyConflict 或接入层唯一约束判断。
func BuildConfirmRequestHash(previewID string, req ConfirmRequest) (RequestHash, error) {
	previewID = strings.TrimSpace(firstNonEmpty(req.PreviewID, previewID))
	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	if previewID == "" {
		return RequestHash{}, newApplyError(ErrorCodeInvalidRequest, "preview_id 不能为空", nil)
	}
	if idempotencyKey == "" {
		return RequestHash{}, newApplyError(ErrorCodeInvalidRequest, "idempotency_key 不能为空", nil)
	}

	bodyHash, err := hashJSON(confirmRequestBodyForHash{
		CandidateID:    strings.TrimSpace(req.CandidateID),
		Action:         normalizeConfirmAction(req.Action),
		EditedChanges:  req.EditedChanges,
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		return RequestHash{}, err
	}

	applyHash := sha256Text(previewID + "\n" + idempotencyKey + "\n" + bodyHash)
	applyID := "asap_" + applyHash[:24]
	return RequestHash{
		BodyHash:  bodyHash,
		ApplyHash: applyHash,
		ApplyID:   applyID,
	}, nil
}

// BuildNormalizedChangesHash 计算转换后 changes 的稳定摘要。
//
// 职责边界：
// 1. 只用于审计和幂等辅助，不替代正式 DB 重校验；
// 2. 输入应是 NormalizeChanges 后的结果，避免相同语义因顺序不同得到不同摘要；
// 3. 序列化失败会返回 invalid_request，调用方应拒绝本次 confirm。
func BuildNormalizedChangesHash(changes []ApplyChange) (string, error) {
	return hashJSON(changes)
}

type confirmRequestBodyForHash struct {
	CandidateID    string        `json:"candidate_id"`
	Action         ConfirmAction `json:"action"`
	EditedChanges  []ApplyChange `json:"edited_changes,omitempty"`
	IdempotencyKey string        `json:"idempotency_key"`
}

func hashJSON(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", newApplyError(ErrorCodeInvalidRequest, "请求体无法生成稳定摘要", err)
	}
	return sha256Bytes(raw), nil
}

func sha256Text(text string) string {
	return sha256Bytes([]byte(text))
}

func sha256Bytes(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func normalizeConfirmAction(action ConfirmAction) ConfirmAction {
	if action == "" {
		return ConfirmActionConfirm
	}
	return action
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
