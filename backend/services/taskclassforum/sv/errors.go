package sv

import (
	"errors"

	"github.com/LoveLosita/smartflow/backend/shared/respond"
)

var (
	// ErrTaskClassPortMissing 表示计划广场需要访问旧 TaskClass，但 adapter 尚未注入。
	ErrTaskClassPortMissing = errors.New("taskclassforum taskclass adapter is nil")

	// ErrForumTagsRequired 表示发布帖子时至少要选择一个标签。
	//
	// 说明：
	// 1. 复用 MissingParam 的状态码，保持 RPC/HTTP 错误映射链路不变；
	// 2. 单独覆写 info，保证前端能直接展示更明确的中文提示；
	// 3. 仅用于“标签必填”这条业务规则，不替代其他参数校验。
	ErrForumTagsRequired = respond.Response{
		Status: respond.MissingParam.Status,
		Info:   "至少选择一个标签",
	}
)
