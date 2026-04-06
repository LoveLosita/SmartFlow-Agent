// Package respond 响应处理
// 统一API响应格式和处理逻辑
package respond

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

type Response struct { //响应结构体
	Status string `json:"status"`
	Info   string `json:"info"`
}

type FinalResponse struct { //最终响应结构体
	Status string      `json:"status"`
	Info   string      `json:"info"`
	Data   interface{} `json:"data"`
}

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

func DealWithError(c *gin.Context, err error) { //处理错误，返回对应的响应结构体
	if err == nil {
		return
	}
	var resp Response
	if errors.Is(err, UserTasksEmpty) || errors.Is(err, NoOngoingOrUpcomingSchedule) {
		c.JSON(http.StatusOK, err)
		return
	}
	if errors.As(err, &resp) {
		c.JSON(http.StatusBadRequest, resp)
		return
	}
	c.JSON(http.StatusInternalServerError, InternalError(err))
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

	NoOngoingOrUpcomingSchedule = Response{ //没有正在进行或即将开始的日程
		Status: "10002",
		Info:   "no ongoing or upcoming schedule",
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

	WrongScheduleEventID = Response{ //日程ID错误
		Status: "40031",
		Info:   "wrong schedule_event id",
	}

	TargetScheduleNotHaveEmbeddedTask = Response{ //目标日程没有嵌入任务
		Status: "40032",
		Info:   "target schedule does not have embedded task",
	}
	TooManyRequests = Response{ //请求过多
		Status: "40033",
		Info:   "too many requests",
	}

	TaskClassItemAlreadyArranged = Response{ //任务类项目已安排
		Status: "40034",
		Info:   "task class item already arranged",
	}

	TargetTaskNotEmbeddedInAnySchedule = Response{ //目标任务未嵌入任何日程
		Status: "40035",
		Info:   "target task not embedded in any schedule",
	}

	TaskClassItemNotFound = Response{ //任务类项目未找到
		Status: "40036",
		Info:   "task class item not found",
	}

	MissingIdempotencyKey = Response{ //缺少幂等性键
		Status: "40037",
		Info:   "missing idempotency key",
	}

	RequestIsProcessing = Response{ //请求正在处理中
		Status: "40038",
		Info:   "request is processing, please do not repeat click",
	}

	TaskClassNotBelongToUser = Response{ //任务类不属于用户
		Status: "40039",
		Info:   "task class does not belong to user",
	}

	WrongTaskClassID = Response{ //任务类ID错误
		Status: "40040",
		Info:   "wrong task class id",
	}

	InvalidSectionNumber = Response{ //无效的节次
		Status: "40041",
		Info:   "invalid section number",
	}

	InvalidWeekOrDayOfWeek = Response{ //无效的周数或星期
		Status: "40042",
		Info:   "invalid week or day_of_week",
	}

	InvalidSectionRange = Response{ //无效的节次范围
		Status: "40043",
		Info:   "invalid section range, start_section should be less than or equal to end_section",
	}

	MissingParamForAutoScheduling = Response{ //自动排课缺少参数
		Status: "40044",
		Info:   "missing param for auto scheduling",
	}

	InvalidDateRange = Response{ //无效的日期范围
		Status: "40045",
		Info:   "invalid date range, start_date should be before or equal to end_date",
	}

	TaskClassModeNotAuto = Response{ //任务类模式不是自动
		Status: "40046",
		Info:   "task class mode is not auto",
	}

	TimeNotEnoughForAutoScheduling = Response{ //自动排课时间不足
		Status: "40047",
		Info:   "time not enough for auto scheduling",
	}
	TaskClassItemNotBelongToTaskClass = Response{ //任务类项目不属于任务类
		Status: "40048",
		Info:   "task class item does not belong to task class",
	}

	TaskClassItemTryingToInsertOutOfTimeRange = Response{ //任务类项目试图插入超出时间范围
		Status: "40049",
		Info:   "task class item trying to insert out of time range",
	}

	WrongTaskID = Response{ //任务ID错误
		Status: "40050",
		Info:   "wrong task id",
	}

	TokenUsageExceedsLimit = Response{ //token 使用量超过限额
		Status: "40051",
		Info:   "token usage exceeds limit",
	}

	TaskNotCompleted = Response{ //任务未完成，无法取消勾选
		Status: "40052",
		Info:   "task is not completed",
	}

	SchedulePlanPreviewNotFound = Response{ //排程预览不存在或已过期
		Status: "40053",
		Info:   "schedule plan preview not found",
	}

	MissingConversationID = Response{ //确认/恢复请求缺少会话ID
		Status: "40054",
		Info:   "conversation_id is required when confirm_action is present",
	}

	RouteControlInternalError = Response{ //路由控制码内部错误
		Status: "50001",
		Info:   "route control failed",
	}

	ScheduleRefineOutputParseFailed = Response{ //智能微调输出二次解析失败
		Status: "50002",
		Info:   "schedule refine output parse failed",
	}
)
