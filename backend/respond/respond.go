// Package respond 响应处理
// 统一API响应格式和处理逻辑
package respond

type Response struct { //响应结构体
	Status string `json:"status"`
	Info   string `json:"info"`
}

type FinalResponse struct { //最终响应结构体
	Status string      `json:"status"`
	Info   string      `json:"info"`
	Data   interface{} `json:"data"`
}

// 实现error接口
func (r Response) Error() string { // 实现 error 接口
	return r.Info
}

func OKWithData(response Response, data interface{}) FinalResponse { //传入一个响应结构体和数据，返回一个最终响应结构体
	var finalResponse FinalResponse
	finalResponse.Status = response.Status
	finalResponse.Info = response.Info
	finalResponse.Data = data
	return finalResponse
}

func InternalError(err error) Response { //服务器错误
	return Response{
		Status: "500",
		Info:   err.Error(),
	}
}

var ( //请求相关的响应
	Ok = Response{ //正常
		Status: "10000",
		Info:   "success",
	}

	WrongName = Response{ //用户名错误
		Status: "40001",
		Info:   "wrong username",
	}

	WrongPwd = Response{ //密码错误
		Status: "40002",
		Info:   "wrong password",
	}

	InvalidName = Response{ //用户名无效
		Status: "40003",
		Info:   "the username already exists",
	}

	MissingParam = Response{ //缺少参数
		Status: "40004",
		Info:   "missing param",
	}

	WrongParamType = Response{ //参数错误
		Status: "40005",
		Info:   "wrong param type",
	}

	ParamTooLong = Response{ //参数过长
		Status: "40006",
		Info:   "param too long",
	}

	WrongUsernameOrPwd = Response{ //用户名或密码错误
		Status: "40007",
		Info:   "wrong username or password",
	}

	WrongGender = Response{ //性别错误
		Status: "40008",
		Info:   "wrong gender",
	}

	MissingToken = Response{ //缺少token
		Status: "40009",
		Info:   "missing token",
	}

	InvalidTokenSingingMethod = Response{ //jwt token签名方法无效
		Status: "40010",
		Info:   "invalid signing method",
	}

	InvalidToken = Response{ //无效token
		Status: "40011",
		Info:   "invalid token",
	}

	InvalidClaims = Response{ //无效声明
		Status: "40012",
		Info:   "invalid claims",
	}

	WrongUserID = Response{ //用户ID错误
		Status: "40013",
		Info:   "wrong userid",
	}

	ErrUnauthorized = Response{ //未授权，没有权限
		Status: "40014",
		Info:   "unauthorized",
	}

	InvalidRefreshToken = Response{ //刷新令牌无效
		Status: "40015",
		Info:   "invalid refresh token",
	}

	WrongTokenType = Response{ //无效令牌类型
		Status: "40016",
		Info:   "wrong token type",
	}
)
