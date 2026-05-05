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
	client ports.UserCommandClient
}

// NewUserHandler 只接收 user/auth 客户端，不再直接依赖本地 user service。
func NewUserHandler(client ports.UserCommandClient) *UserHandler {
	return &UserHandler{client: client}
}

func (api *UserHandler) UserRegister(c *gin.Context) {
	var req contracts.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	retUser, err := api.client.Register(ctx, req)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, retUser))
}

func (api *UserHandler) UserLogin(c *gin.Context) {
	var req contracts.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	tokens, err := api.client.Login(ctx, req)
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
