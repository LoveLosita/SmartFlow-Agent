package userauthapi

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/gateway/shared/respond"
	"github.com/spf13/viper"
)

const (
	geeTestRegisterURL = "https://api.geetest.com/register.php"
	geeTestValidateURL = "http://api.geetest.com/validate.php"
	geeTestClientType  = "web"
	geeTestSDKName     = "smartflow-gateway-go/1.0"
)

type geeTestRegisterUpstreamResponse struct {
	Challenge string `json:"challenge"`
}

type geeTestValidateUpstreamResponse struct {
	Seccode string `json:"seccode"`
}

// GeeTestService 负责封装 gateway 与极验 v3 接口的最小交互。
// 职责边界：
// 1. 只处理验证码初始化与二次校验，不承载登录/注册业务；
// 2. 只暴露 gateway HTTP 层真正需要的最小方法，避免把第三方协议散落到 handler；
// 3. 不做离线 failback 存储，当前阶段聚焦“在线校验闭环”这一个能力域。
type GeeTestService struct {
	captchaID  string
	privateKey string
	httpClient *http.Client
}

func NewGeeTestServiceFromConfig() *GeeTestService {
	return &GeeTestService{
		captchaID:  strings.TrimSpace(viper.GetString("geetest.captchaID")),
		privateKey: strings.TrimSpace(viper.GetString("geetest.privateKey")),
		httpClient: &http.Client{Timeout: 3 * time.Second},
	}
}

// Register 负责向极验申请当前页 challenge，并转成前端 `initGeetest` 可直接消费的结构。
// 职责边界：
// 1. 只对应官方 API1 初始化；
// 2. 不负责缓存 challenge，也不负责表单业务字段；
// 3. 若极验服务不可用，直接返回初始化失败，让前端走显式提示。
func (s *GeeTestService) Register(ctx context.Context, clientIP string) (*captchaRegisterResponse, error) {
	if !s.isConfigured() {
		return nil, respond.CaptchaInitFailed
	}

	// 1. 先按官方 API1 约定拉取原始 challenge。
	// 2. 再使用 privateKey 做一次签名混淆，避免把上游原始 challenge 直接暴露给前端。
	// 3. 任一步失败都直接中断，让登录/注册入口显式暴露初始化异常。
	query := url.Values{}
	query.Set("digestmod", "md5")
	query.Set("gt", s.captchaID)
	query.Set("json_format", "1")
	query.Set("sdk", geeTestSDKName)
	query.Set("client_type", geeTestClientType)
	if ip := strings.TrimSpace(clientIP); ip != "" {
		query.Set("ip_address", ip)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, geeTestRegisterURL+"?"+query.Encode(), nil)
	if err != nil {
		return nil, respond.CaptchaInitFailed
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, respond.CaptchaInitFailed
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, respond.CaptchaInitFailed
	}

	challenge := extractRegisterChallenge(rawBody)
	if challenge == "" {
		return nil, respond.CaptchaInitFailed
	}

	return &captchaRegisterResponse{
		Success:    1,
		GT:         s.captchaID,
		Challenge:  md5Hex(challenge + s.privateKey),
		NewCaptcha: true,
	}, nil
}

// Verify 负责校验前端回传的极验三元组。
// 职责边界：
// 1. 先做本地 challenge/validate 一致性检查，尽早拦截无效请求；
// 2. 再调用官方 API2 校验 seccode，保证验证码结果真实有效；
// 3. 只返回“通过/失败/服务不可用”三类结论，不混入登录注册业务判断。
func (s *GeeTestService) Verify(ctx context.Context, payload geeTestValidatePayload, clientIP string) error {
	if !s.isConfigured() {
		return respond.CaptchaVerifyUnavailable
	}

	challenge := strings.TrimSpace(payload.Challenge)
	validate := strings.TrimSpace(payload.Validate)
	seccode := strings.TrimSpace(payload.Seccode)
	if challenge == "" || validate == "" || seccode == "" {
		return respond.MissingParam
	}

	// 1. 先按极验 v3 协议校验 validate 是否与 challenge/privateKey 匹配。
	// 2. 若本地签名都对不上，直接判失败，避免继续请求第三方接口。
	// 3. 只有本地签名通过后，才继续调用 API2 复核 seccode。
	expectedValidate := md5Hex(s.privateKey + "geetest" + challenge)
	if !strings.EqualFold(validate, expectedValidate) {
		return respond.CaptchaVerifyFailed
	}

	form := url.Values{}
	form.Set("captchaid", s.captchaID)
	form.Set("challenge", challenge)
	form.Set("seccode", seccode)
	form.Set("json_format", "1")
	form.Set("sdk", geeTestSDKName)
	form.Set("client_type", geeTestClientType)
	if ip := strings.TrimSpace(clientIP); ip != "" {
		form.Set("ip_address", ip)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, geeTestValidateURL, strings.NewReader(form.Encode()))
	if err != nil {
		return respond.CaptchaVerifyUnavailable
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return respond.CaptchaVerifyUnavailable
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return respond.CaptchaVerifyUnavailable
	}

	if !matchValidateSeccode(rawBody, seccode) {
		return respond.CaptchaVerifyFailed
	}
	return nil
}

func (s *GeeTestService) isConfigured() bool {
	return s != nil && s.captchaID != "" && s.privateKey != ""
}

func extractRegisterChallenge(rawBody []byte) string {
	var payload geeTestRegisterUpstreamResponse
	if err := json.Unmarshal(rawBody, &payload); err == nil {
		return strings.TrimSpace(payload.Challenge)
	}
	return strings.TrimSpace(string(rawBody))
}

func matchValidateSeccode(rawBody []byte, seccode string) bool {
	expected := md5Hex(strings.TrimSpace(seccode))

	var payload geeTestValidateUpstreamResponse
	if err := json.Unmarshal(rawBody, &payload); err == nil {
		return strings.EqualFold(strings.TrimSpace(payload.Seccode), expected)
	}
	return strings.EqualFold(strings.TrimSpace(string(rawBody)), expected)
}

func md5Hex(input string) string {
	sum := md5.Sum([]byte(input))
	return hex.EncodeToString(sum[:])
}
