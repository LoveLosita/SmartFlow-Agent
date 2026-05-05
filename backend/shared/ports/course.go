package ports

import (
	"context"
	"encoding/json"

	coursecontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/course"
)

// CourseCommandClient 是 gateway 调用 course 服务的最小能力集合。
//
// 职责边界：
// 1. 只覆盖当前 `/api/v1/course/*` HTTP 门面需要的能力；
// 2. 不暴露 course DAO，也不暴露迁移期直写 schedule 表的实现细节；
// 3. import / parse 的复杂响应以 JSON 透传，避免 gateway 复制业务模型。
type CourseCommandClient interface {
	ValidateCourse(ctx context.Context, req coursecontracts.UserCheckCourseRequest) error
	ImportCourses(ctx context.Context, req coursecontracts.UserImportCoursesRequest) (json.RawMessage, error)
	ParseCourseTableImage(ctx context.Context, req coursecontracts.CourseImageParseRequest) (json.RawMessage, error)
}
