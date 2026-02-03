package dao

import (
	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
	"gorm.io/gorm"
)

type TaskDAO struct {
	// 这是一个口袋，用来装数据库连接实例
	db *gorm.DB
}

// NewTaskDAO 创建TaskDAO实例
// NewTaskDAO 接收一个 *gorm.DB，并把它塞进结构体的口袋里
func NewTaskDAO(db *gorm.DB) *TaskDAO {
	return &TaskDAO{
		db: db,
	}
}

// AddTask 为指定用户添加任务
func (dao *TaskDAO) AddTask(req *model.Task) (*model.Task, error) {
	if err := dao.db.Create(req).Error; err != nil {
		return nil, err
	}
	return req, nil
}

func (dao *TaskDAO) GetTasksByUserID(userID int) ([]model.Task, error) {
	var tasks []model.Task
	if err := dao.db.Where("user_id = ?", userID).Find(&tasks).Error; err != nil {
		return nil, err
	}
	if len(tasks) == 0 { // 如果没有任务，返回自定义错误
		return nil, respond.UserTasksEmpty
	}
	return tasks, nil
}
