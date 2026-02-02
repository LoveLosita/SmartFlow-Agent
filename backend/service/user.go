// Package service 业务逻辑层
// 包含所有核心业务逻辑
package service

import (
	"errors"

	"github.com/golang-jwt/jwt/v4"
	"github.com/smartflow/backend/auth"
	"github.com/smartflow/backend/dao"
	"github.com/smartflow/backend/model"
	"github.com/smartflow/backend/respond"
	"github.com/smartflow/backend/utils"
	"gorm.io/gorm"
)

type UserService struct {
	// 伸出手：准备接住 DAO
	repo *dao.UserDAO
}

// NewUserService：组装 Service 的“工厂”
func NewUserService(repo *dao.UserDAO) *UserService {
	return &UserService{
		repo: repo, // 把传进来的 DAO 揣进口袋里
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
	result, err := sv.repo.IfUsernameExists(user.Username)
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
	newUser, err := sv.repo.Create(user.Username, user.PhoneNumber, user.Password)
	if err != nil {
		return nil, err
	}
	//返回注册成功的用户ID
	return &model.UserRegisterResponse{ID: newUser.ID}, nil
}

func (sv *UserService) UserLogin(req *model.UserLoginRequest) (*model.Tokens, error) {
	var tokens model.Tokens
	hashedPwd, err := sv.repo.GetUserHashedPasswordByName(req.Username) //调用dao层的方法
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
	id, err := sv.repo.GetUserIDByName(req.Username)
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

func (sv *UserService) RefreshTokenHandler(refreshToken string) (*model.Tokens, error) {
	// 验证刷新令牌
	token, err := auth.ValidateRefreshToken(refreshToken)
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
}
