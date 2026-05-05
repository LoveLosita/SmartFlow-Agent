package middleware

import (
	"errors"
	"net/http"

	"github.com/LoveLosita/smartflow/backend/gateway/shared/respond"
	"github.com/gin-gonic/gin"
)

// writeRespondError 负责把项目内 respond.Response 统一写回 HTTP。
//
// 职责边界：
// 1. 只处理 respond.Response / 普通 error 到 HTTP JSON 的映射；
// 2. 不关心调用方来自哪个中间件，也不关心上游业务属于鉴权还是额度控制；
// 3. 方便多个 gateway 中间件复用同一套错误写回规则。
func writeRespondError(c *gin.Context, err error) {
	if err == nil {
		return
	}

	var resp respond.Response
	if errors.As(err, &resp) {
		c.JSON(resp.HTTPStatus(), resp)
		return
	}

	c.JSON(http.StatusInternalServerError, respond.InternalError(err))
}
