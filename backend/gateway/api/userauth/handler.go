package userauthapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	gatewaymiddleware "github.com/LoveLosita/smartflow/backend/gateway/middleware"
	"github.com/LoveLosita/smartflow/backend/gateway/shared/respond"
	contracts "github.com/LoveLosita/smartflow/backend/shared/contracts/userauth"
	"github.com/LoveLosita/smartflow/backend/shared/ports"
	"github.com/gin-gonic/gin"
)

type UserHandler struct {
	client        ports.UserCommandClient
	captcha       *GeeTestService
	allowRegister bool
}

// NewUserHandler 只接收 user/auth 客户端与验证码服务，不再直接依赖本地 user service。
func NewUserHandler(client ports.UserCommandClient, captcha *GeeTestService, allowRegister bool) *UserHandler {
	return &UserHandler{
		client:        client,
		captcha:       captcha,
		allowRegister: allowRegister,
	}
}

func (api *UserHandler) CaptchaRegister(c *gin.Context) {
	captchaCtx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()

	registerData, err := api.captcha.Register(captchaCtx, c.ClientIP())
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, registerData))
}

func (api *UserHandler) UserRegister(c *gin.Context) {
	if !api.ensureRegisterEnabled(c) {
		return
	}

	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}

	// 1. 先用独立超时完成极验二次校验，避免第三方接口抖动侵入内部 RPC 超时预算。
	// 2. 只有验证码通过后才继续调 user/auth 注册服务，防止无效流量进入内部链路。
	// 3. 内部 RPC 仍保留原先 2 秒超时边界，不改变现有 user/auth 服务 SLA。
	captchaCtx, cancelCaptcha := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancelCaptcha()
	if err := api.captcha.Verify(captchaCtx, req.captchaPayload(), c.ClientIP()); err != nil {
		respond.DealWithError(c, err)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	retUser, err := api.client.Register(ctx, req.toContract())
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, retUser))
}

func (api *UserHandler) UserLogin(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}

	captchaCtx, cancelCaptcha := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancelCaptcha()
	if err := api.captcha.Verify(captchaCtx, req.captchaPayload(), c.ClientIP()); err != nil {
		respond.DealWithError(c, err)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	tokens, err := api.client.Login(ctx, req.toContract())
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, tokens))
}

func (api *UserHandler) RefreshTokenHandler(c *gin.Context) {
	var req contracts.RefreshTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	if strings.TrimSpace(req.RefreshToken) == "" {
		c.JSON(http.StatusBadRequest, respond.MissingParam)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	tokens, err := api.client.RefreshToken(ctx, req)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, tokens))
}

func (api *UserHandler) UserLogout(c *gin.Context) {
	token := gatewaymiddleware.ExtractTokenFromAuthorization(c.GetHeader("Authorization"))
	if token == "" {
		c.JSON(http.StatusUnauthorized, respond.MissingToken)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	if err := api.client.Logout(ctx, token); err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.Ok)
}

// ensureRegisterEnabled 负责统一收口“注册相关入口”的开关判断。
// 职责边界：
// 1. 只判断当前环境是否允许注册，并向前端返回明确的 403；
// 2. 不负责登录、刷新 token、登出等其它 user/auth 能力；
// 3. 不触发验证码或 RPC 调用，避免“已关闭注册”时仍向下游产生无效流量。
func (api *UserHandler) ensureRegisterEnabled(c *gin.Context) bool {
	if api.allowRegister {
		return true
	}

	c.JSON(http.StatusForbidden, respond.Response{
		Status: "40301",
		Info:   "registration is disabled",
	})
	return false
}
