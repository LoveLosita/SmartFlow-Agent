package api

type ApiHandlers struct {
	UserHandler      *UserHandler
	TaskHandler      *TaskHandler
	CourseHandler    *CourseHandler
	TaskClassHandler *TaskClassHandler
	ScheduleHandler  *ScheduleAPI
	AgentHandler     *AgentHandler
	MemoryHandler    *MemoryHandler
}
