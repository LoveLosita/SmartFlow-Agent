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

func RespWithData(response Response, data interface{}) FinalResponse { //传入一个响应结构体和数据，返回一个最终响应结构体
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

	UserTasksEmpty = Response{ //用户任务为空
		Status: "10001",
		Info:   "user tasks empty",
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

	UserLoggedOut = Response{ //用户已登出
		Status: "40017",
		Info:   "user logged out",
	}

	InvalidPriority = Response{ //无效优先级
		Status: "40018",
		Info:   "invalid priority",
	}

	WrongCourseInfo = Response{ //课程信息错误
		Status: "40019",
		Info:   "wrong course info",
	}

	UserTaskClassNotFound = Response{ //用户任务类未找到
		Status: "40020",
		Info:   "user task class not found",
	}

	UserTaskClassForbidden = Response{ //用户任务类禁止访问
		Status: "40021",
		Info:   "user task class forbidden",
	}

	TaskClassItemNotBelongToUser = Response{ //任务类项目不属于用户
		Status: "40022",
		Info:   "task class item does not belong to user",
	}

	TimeOutOfRangeOfThisSemester = Response{ //时间超出本学期范围
		Status: "40023",
		Info:   "time out of range of this semester",
	}

	CourseNotBelongToUser = Response{ //课程不属于用户
		Status: "40024",
		Info:   "course does not belong to user",
	}

	CourseAlreadyEmbeddedByOtherTaskBlock = Response{ //课程已被其他任务块嵌入
		Status: "40025",
		Info:   "course already embedded by other task block",
	}

	ScheduleConflict = Response{ //日程冲突
		Status: "40026",
		Info:   "schedule conflict",
	}

	WrongCourseID = Response{ //课程ID错误
		Status: "40027",
		Info:   "wrong course id",
	}

	CourseTimeNotMatch = Response{ //课程时间不匹配
		Status: "40028",
		Info:   "course time not match",
	}

	InsertCourseTwice = Response{ //重复插入课程
		Status: "40029",
		Info:   "insert course twice",
	}

	WeekOutOfRange = Response{ //周数超出范围
		Status: "40030",
		Info:   "week out of range",
	}
)
