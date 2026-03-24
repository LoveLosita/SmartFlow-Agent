import http from '@/api/http'
import type { ApiResponse } from '@/types/api'
import type { TaskCreatePayload, TaskCreateResult, TaskItem, TaskMutationResult } from '@/types/dashboard'
import { createIdempotencyKey } from '@/utils/idempotency'
import { extractErrorMessage } from '@/utils/http'

export async function getTasks() {
  try {
    const response = await http.get<ApiResponse<TaskItem[]>>('/task/get')
    return response.data.data ?? []
  } catch (error) {
    throw new Error(extractErrorMessage(error, '任务加载失败，请稍后重试'))
  }
}

export async function createTask(payload: TaskCreatePayload) {
  try {
    const response = await http.post<ApiResponse<TaskCreateResult>>('/task/create', payload, {
      headers: {
        'X-Idempotency-Key': createIdempotencyKey('task-create'),
      },
    })
    return response.data.data
  } catch (error) {
    throw new Error(extractErrorMessage(error, '创建任务失败，请稍后重试'))
  }
}

export async function completeTask(taskId: number) {
  try {
    const response = await http.put<ApiResponse<TaskMutationResult>>(
      '/task/complete',
      { task_id: taskId },
      {
        headers: {
          'X-Idempotency-Key': createIdempotencyKey('task-complete'),
        },
      },
    )
    return response.data.data
  } catch (error) {
    throw new Error(extractErrorMessage(error, '更新任务状态失败，请稍后重试'))
  }
}

export async function undoCompleteTask(taskId: number) {
  try {
    const response = await http.put<ApiResponse<TaskMutationResult>>(
      '/task/undo-complete',
      { task_id: taskId },
      {
        headers: {
          'X-Idempotency-Key': createIdempotencyKey('task-undo'),
        },
      },
    )
    return response.data.data
  } catch (error) {
    throw new Error(extractErrorMessage(error, '恢复任务失败，请稍后重试'))
  }
}
