package api

type ApiHandlers struct {
	TaskHandler      *TaskHandler
	CourseHandler    *CourseHandler
	TaskClassHandler *TaskClassHandler
	ScheduleHandler  *ScheduleAPI
	AgentHandler     *AgentHandler
	MemoryHandler    *MemoryHandler
	ActiveSchedule   *ActiveScheduleAPI
	Notification     *NotificationAPI
}
