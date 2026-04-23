package conv

import (
	"time"

	"github.com/LoveLosita/smartflow/backend/model"
)

func UserAddTaskRequestToModel(request *model.UserAddTaskRequest, userID int) *model.Task {
	return &model.Task{
		Title:      request.Title,
		Priority:   request.PriorityGroup,
		DeadlineAt: request.DeadlineAt,
		UserID:     userID,
	}
}

func ModelToUserAddTaskResponse(task *model.Task) *model.UserAddTaskResponse {
	status := "incomplete"
	if task.IsCompleted {
		status = "completed"
	}
	return &model.UserAddTaskResponse{
		ID:            task.ID,
		Title:         task.Title,
		PriorityGroup: task.Priority,
		DeadlineAt:    task.DeadlineAt,
		Status:        status,
		CreatedAt:     time.Now(), // 创建时间为当前时间
	}
}

func ModelToGetUserTasksResp(tasks []model.Task) []model.GetUserTaskResp {
	var resp []model.GetUserTaskResp
	for _, task := range tasks {
		status := "incomplete"
		if task.IsCompleted {
			status = "completed"
		}

		deadline := ""
		if task.DeadlineAt != nil {
			deadline = task.DeadlineAt.Format("2006-01-02 15:04:05")
		}

		resp = append(resp, model.GetUserTaskResp{
			ID:            task.ID,
			UserID:        task.UserID,
			Title:         task.Title,
			PriorityGroup: task.Priority,
			Status:        status,
			Deadline:      deadline,
			IsCompleted:   task.IsCompleted,
		})
	}
	return resp
}

// ModelToGetUserTaskResp 将单个 Task 模型转换为 GetUserTaskResp。
func ModelToGetUserTaskResp(task *model.Task) model.GetUserTaskResp {
	status := "incomplete"
	if task.IsCompleted {
		status = "completed"
	}
	deadline := ""
	if task.DeadlineAt != nil {
		deadline = task.DeadlineAt.Format("2006-01-02 15:04:05")
	}
	return model.GetUserTaskResp{
		ID:            task.ID,
		UserID:        task.UserID,
		Title:         task.Title,
		PriorityGroup: task.Priority,
		Status:        status,
		Deadline:      deadline,
		IsCompleted:   task.IsCompleted,
	}
}
