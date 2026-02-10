// Package api 定义API接口层
// 包含所有对外暴露的HTTP接口定义
package api

import (
	"context"
	"net/http"
	"time"

	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
	"github.com/LoveLosita/smartflow/backend/service"
	"github.com/gin-gonic/gin"
)

type UserHandler struct {
	// 伸出手：准备接住 Service
	svc *service.UserService
}

// NewUserHandler：组装 Handler 的“工厂”
func NewUserHandler(svc *service.UserService) *UserHandler {
	return &UserHandler{
		svc: svc, // 把传进来的 Service 揣进口袋里
	}
}

// UserRegister 用户注册API
// 处理用户注册请求
func (api *UserHandler) UserRegister(c *gin.Context) {
	var user model.UserRegisterRequest
	err := c.ShouldBindJSON(&user)
	if err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	// 创建一个带 1 秒超时的上下文
	ctx, cancel := context.WithTimeout(c.Request.Context(), 1*time.Second)
	defer cancel() // 记得释放资源
	retUser, err := api.svc.UserRegister(ctx, user)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}

	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, retUser))
}

func (api *UserHandler) UserLogin(c *gin.Context) {
	var req model.UserLoginRequest
	err := c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(http.StatusOK, respond.WrongParamType)
		return
	}
	// 创建一个带 1 秒超时的上下文
	ctx, cancel := context.WithTimeout(c.Request.Context(), 1*time.Second)
	defer cancel() // 记得释放资源
	tokens, err := api.svc.UserLogin(ctx, &req)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, tokens))
}

func (api *UserHandler) RefreshTokenHandler(c *gin.Context) {
	var requestBody struct {
		RefreshToken string `json:"old_refresh_token"`
	}
	if err := c.ShouldBindJSON(&requestBody); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	if requestBody.RefreshToken == "" {
		c.JSON(http.StatusBadRequest, respond.MissingParam)
	}
	// 创建一个带 1 秒超时的上下文
	ctx, cancel := context.WithTimeout(c.Request.Context(), 1*time.Second)
	defer cancel() // 记得释放资源
	tokens, err := api.svc.RefreshTokenHandler(ctx, requestBody.RefreshToken)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, tokens))
}

func (api *UserHandler) UserLogout(c *gin.Context) {
	//1.从上下文中获取 jti 和 expireTime
	claims, _ := c.Get("claims")
	cl := claims.(*model.MyCustomClaims)
	//2.调用 Service 层的 UserLogout 方法
	// 创建一个带 1 秒超时的上下文
	ctx, cancel := context.WithTimeout(c.Request.Context(), 1*time.Second)
	defer cancel() // 记得释放资源
	err := api.svc.UserLogout(ctx, cl.Jti, cl.ExpiresAt.Time)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.Ok)
}
