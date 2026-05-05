package conv

import (
	"time"

	"github.com/LoveLosita/smartflow/backend/model"
)

func UserAddTaskRequestToModel(request *model.UserAddTaskRequest, userID int) *model.Task {
	return &model.Task{
		Title:              request.Title,
		Priority:           request.PriorityGroup,
		EstimatedSections:  model.NormalizeEstimatedSections(&request.EstimatedSections),
		DeadlineAt:         request.DeadlineAt,
		UrgencyThresholdAt: request.UrgencyThresholdAt,
		UserID:             userID,
	}
}

func ModelToUserAddTaskResponse(task *model.Task) *model.UserAddTaskResponse {
	status := "incomplete"
	if task.IsCompleted {
		status = "completed"
	}
	return &model.UserAddTaskResponse{
		ID:                task.ID,
		Title:             task.Title,
		PriorityGroup:     task.Priority,
		EstimatedSections: model.NormalizeEstimatedSections(&task.EstimatedSections),
		DeadlineAt:        task.DeadlineAt,
		Status:            status,
		CreatedAt:         time.Now(), // 创建时间使用当前服务时间，保持既有响应语义。
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

		urgencyThreshold := ""
		if task.UrgencyThresholdAt != nil {
			urgencyThreshold = task.UrgencyThresholdAt.Format("2006-01-02 15:04:05")
		}

		resp = append(resp, model.GetUserTaskResp{
			ID:                 task.ID,
			UserID:             task.UserID,
			Title:              task.Title,
			PriorityGroup:      task.Priority,
			EstimatedSections:  model.NormalizeEstimatedSections(&task.EstimatedSections),
			Status:             status,
			Deadline:           deadline,
			IsCompleted:        task.IsCompleted,
			UrgencyThresholdAt: urgencyThreshold,
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
	urgencyThreshold := ""
	if task.UrgencyThresholdAt != nil {
		urgencyThreshold = task.UrgencyThresholdAt.Format("2006-01-02 15:04:05")
	}
	return model.GetUserTaskResp{
		ID:                 task.ID,
		UserID:             task.UserID,
		Title:              task.Title,
		PriorityGroup:      task.Priority,
		EstimatedSections:  model.NormalizeEstimatedSections(&task.EstimatedSections),
		Status:             status,
		Deadline:           deadline,
		IsCompleted:        task.IsCompleted,
		UrgencyThresholdAt: urgencyThreshold,
	}
}
