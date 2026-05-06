package userauthapi

import contracts "github.com/LoveLosita/smartflow/backend/shared/contracts/userauth"

// geeTestValidatePayload 只承载 gateway 边界的人机验证字段。
// 职责边界：
// 1. 负责承接前端提交的 geetest 三元组；
// 2. 不负责 user/auth RPC 入参映射，避免把第三方验证码字段带入内部服务契约；
// 3. 不负责校验逻辑，真正校验由 GeeTestService 完成。
type geeTestValidatePayload struct {
	Challenge string `json:"geetest_challenge"`
	Validate  string `json:"geetest_validate"`
	Seccode   string `json:"geetest_seccode"`
}

type registerRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	PhoneNumber string `json:"phone_number"`

	geeTestValidatePayload
}

func (r registerRequest) toContract() contracts.RegisterRequest {
	return contracts.RegisterRequest{
		Username:    r.Username,
		Password:    r.Password,
		PhoneNumber: r.PhoneNumber,
	}
}

func (r registerRequest) captchaPayload() geeTestValidatePayload {
	return r.geeTestValidatePayload
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`

	geeTestValidatePayload
}

func (r loginRequest) toContract() contracts.LoginRequest {
	return contracts.LoginRequest{
		Username: r.Username,
		Password: r.Password,
	}
}

func (r loginRequest) captchaPayload() geeTestValidatePayload {
	return r.geeTestValidatePayload
}

type captchaRegisterResponse struct {
	Success    int    `json:"success"`
	GT         string `json:"gt"`
	Challenge  string `json:"challenge"`
	NewCaptcha bool   `json:"new_captcha"`
}
