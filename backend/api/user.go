// Package api 定义API接口层
// 包含所有对外暴露的HTTP接口定义
package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/smartflow/backend/model"
	"github.com/smartflow/backend/respond"
	"github.com/smartflow/backend/service"
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

	retUser, err := api.svc.UserRegister(user)
	if err != nil {
		switch {
		case errors.Is(err, respond.InvalidName), errors.Is(err, respond.MissingParam),
			errors.Is(err, respond.ParamTooLong): //如果是无效ID或者缺少参数的错误
			c.JSON(http.StatusBadRequest, err)
			return
		default:
			c.JSON(http.StatusInternalServerError, respond.InternalError(err))
			return
		}
	}

	c.JSON(http.StatusOK, respond.OKWithData(respond.Ok, retUser))
}

func (api *UserHandler) UserLogin(c *gin.Context) {
	var req model.UserLoginRequest
	err := c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(http.StatusOK, respond.WrongParamType)
		return
	}
	tokens, err := api.svc.UserLogin(&req)
	if err != nil {
		switch {
		case errors.Is(err, respond.WrongName), errors.Is(err, respond.WrongPwd): //如果是无效ID或者缺少参数的错误
			c.JSON(http.StatusBadRequest, err)
			return
		default:
			c.JSON(http.StatusInternalServerError, respond.InternalError(err))
			return
		}
	}
	c.JSON(http.StatusOK, respond.OKWithData(respond.Ok, tokens))
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
	tokens, err := api.svc.RefreshTokenHandler(requestBody.RefreshToken)
	if err != nil {
		switch {
		case errors.Is(err, respond.InvalidRefreshToken), errors.Is(err, respond.InvalidClaims),
			errors.Is(err, respond.InvalidTokenSingingMethod): //如果是无效刷新令牌或者无效claims或者无效签名方法
			c.JSON(http.StatusBadRequest, err)
			return
		default:
			c.JSON(http.StatusInternalServerError, respond.InternalError(err))
		}
	}
	c.JSON(http.StatusOK, respond.OKWithData(respond.Ok, tokens))
}
