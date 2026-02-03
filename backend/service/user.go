// Package service 业务逻辑层
// 包含所有核心业务逻辑
package service

import (
	"errors"
	"time"

	"github.com/LoveLosita/smartflow/backend/auth"
	"github.com/LoveLosita/smartflow/backend/dao"
	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
	"github.com/LoveLosita/smartflow/backend/utils"
	"gorm.io/gorm"
)

type UserService struct {
	userRepo  *dao.UserDAO
	cacheRepo *dao.CacheDAO
}

func NewUserService(userRepo *dao.UserDAO, cacheRepo *dao.CacheDAO) *UserService {
	return &UserService{
		userRepo:  userRepo, // 把传进来的 DAO 揣进口袋里
		cacheRepo: cacheRepo,
	}
}

func (sv *UserService) UserRegister(user model.UserRegisterRequest) (*model.UserRegisterResponse, error) {
	//检查是否有空字段
	if user.Username == "" || user.Password == "" ||
		user.PhoneNumber == "" {
		return nil, respond.MissingParam
	}
	// 检查字段长度是否超过90%
	if len(user.Username) > 45 || len(user.Password) > 229 || len(user.PhoneNumber) > 18 {
		return nil, respond.ParamTooLong
	}
	//检查用户名是否已存在
	result, err := sv.userRepo.IfUsernameExists(user.Username)
	if err != nil {
		return nil, err
	}
	if result {
		return nil, respond.InvalidName
	}
	hashedPwd, err := utils.HashPassword(user.Password) //调用utils层的方法
	if err != nil {
		return nil, err
	}
	user.Password = hashedPwd //将user的密码字段改为加密后的密码
	newUser, err := sv.userRepo.Create(user.Username, user.PhoneNumber, user.Password)
	if err != nil {
		return nil, err
	}
	//返回注册成功的用户ID
	return &model.UserRegisterResponse{ID: newUser.ID}, nil
}

func (sv *UserService) UserLogin(req *model.UserLoginRequest) (*model.Tokens, error) {
	var tokens model.Tokens
	hashedPwd, err := sv.userRepo.GetUserHashedPasswordByName(req.Username) //调用dao层的方法
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, respond.WrongName
		}
		return nil, err
	}
	result, err := utils.CompareHashPwdAndPwd(hashedPwd, req.Password) //比较密码是否匹配
	if err != nil {                                                    //其他错误
		return &tokens, err
	} else if !result { //密码不匹配
		return nil, respond.WrongPwd
	}
	id, err := sv.userRepo.GetUserIDByName(req.Username)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, respond.WrongName
		}
		return nil, err
	}
	tokens.AccessToken, tokens.RefreshToken, err = auth.GenerateTokens(id) //生成jwt key
	if err != nil {                                                        //其他错误
		return nil, err
	}
	return &tokens, nil
}

/*func (sv *UserService) RefreshTokenHandler(refreshToken string) (*model.Tokens, error) {
	// 验证刷新令牌
	token, err := auth.ValidateRefreshToken(refreshToken, sv.cacheRepo)
	if err != nil || !token.Valid { // 刷新令牌无效
		return nil, respond.InvalidRefreshToken
	}

	// 生成新的访问令牌和刷新令牌
	if claims, ok := token.Claims.(jwt.MapClaims); ok {
		userID := int(claims["user_id"].(float64))
		newAccessToken, newRefreshToken, err := auth.GenerateTokens(userID)
		if err != nil {
			return nil, err
		}

		// 返回新的访问令牌和刷新令牌
		return &model.Tokens{AccessToken: newAccessToken, RefreshToken: newRefreshToken}, nil
	} else {
		return nil, respond.InvalidClaims
	}
}*/

func (sv *UserService) RefreshTokenHandler(refreshToken string) (*model.Tokens, error) {
	// 1. 验证刷新令牌 (这里已经包含了 Redis 黑名单检查)
	token, err := auth.ValidateRefreshToken(refreshToken, sv.cacheRepo)
	if err != nil {
		return nil, err
	}

	// 2. 改动点：直接断言为你定义的结构体 model.MyCustomClaims
	if claims, ok := token.Claims.(*model.MyCustomClaims); ok {
		// 3. 这里的 userID 已经是 int 了，不再需要 (float64) 转换
		newAccessToken, newRefreshToken, err := auth.GenerateTokens(claims.UserID)
		if err != nil {
			return nil, err
		}
		// 返回新的双 Token
		return &model.Tokens{
			AccessToken:  newAccessToken,
			RefreshToken: newRefreshToken,
		}, nil
	}

	return nil, respond.InvalidClaims
}

func (sv *UserService) UserLogout(jti string, expireTime time.Time) error {
	//1.直接把 jti 扔进黑名单
	expiration := time.Until(expireTime)
	err := sv.cacheRepo.SetBlacklist(jti, expiration)
	if err != nil {
		return err
	}
	return nil
}
