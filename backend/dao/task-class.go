package dao

import (
	"github.com/LoveLosita/smartflow/backend/model"
	"gorm.io/gorm"
)

type TaskClassDAO struct {
	// 这是一个口袋，用来装数据库连接实例
	db *gorm.DB
}

// NewTaskClassDAO 创建TaskClassDAO实例
// NewTaskClassDAO 接收一个 *gorm.DB，并把它塞进结构体的口袋里
func NewTaskClassDAO(db *gorm.DB) *TaskClassDAO {
	return &TaskClassDAO{
		db: db,
	}
}

// AddTaskClass 为指定用户添加任务类
func (dao *TaskClassDAO) AddTaskClass(taskClass *model.TaskClass) (int, error) {
	err := dao.db.Create(taskClass).Error
	if err != nil {
		return 0, err
	}
	return taskClass.ID, nil
}

func (dao *TaskClassDAO) AddTaskClassItems(items []model.TaskClassItem) error {
	return dao.db.Create(&items).Error
}

// Transaction 在一个事务中执行传入的函数，供 service 层复用（自动提交/回滚）
// 规则：fn 返回 nil -> commit；fn 返回 error 或 panic -> rollback
func (dao *TaskClassDAO) Transaction(fn func(txDAO *TaskClassDAO) error) error {
	return dao.db.Transaction(func(tx *gorm.DB) error {
		return fn(NewTaskClassDAO(tx))
	})
}

func (dao *TaskClassDAO) GetUserTaskClasses(userID int) ([]model.TaskClass, error) {
	var taskClasses []model.TaskClass
	err := dao.db.Where("user_id = ?", userID).Find(&taskClasses).Error
	if err != nil {
		return nil, err
	}
	return taskClasses, nil
}
