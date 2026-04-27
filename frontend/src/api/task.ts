import http from '@/api/http'
import type { ApiResponse } from '@/types/api'
import type { TaskUpdatePayload, TaskCreatePayload, TaskCreateResult, TaskItem, TaskMutationResult } from '@/types/dashboard'
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

export async function updateTask(payload: TaskUpdatePayload) {
  try {
    const response = await http.put<ApiResponse<TaskItem>>(
      '/task/update',
      payload,
      {
        headers: {
          'X-Idempotency-Key': createIdempotencyKey('task-update'),
        },
      }
    )
    return response.data.data
  } catch (error) {
    throw new Error(extractErrorMessage(error, '修改任务失败，请稍后重试'))
  }
}

export interface TaskBatchStatusItem {
  id: number
  is_completed: boolean
}

export interface TaskBatchStatusResult {
  items: TaskBatchStatusItem[]
}

export async function getTaskBatchStatus(ids: number[]) {
  if (ids.length === 0) return []
  try {
    const response = await http.post<ApiResponse<TaskBatchStatusResult>>('/task/batch-status', { ids })
    return response.data.data?.items ?? []
  } catch (error) {
    console.error('Failed to fetch batch status:', error)
    return []
  }
}

export async function deleteTask(taskId: number) {
  try {
    const response = await http.delete<ApiResponse<{ task_id: number }>>(
      '/task/delete',
      {
        data: { task_id: taskId },
        headers: {
          'X-Idempotency-Key': createIdempotencyKey('task-delete'),
        },
      }
    )
    return response.data.data
  } catch (error) {
    throw new Error(extractErrorMessage(error, '删除任务失败，请稍后重试'))
  }
}

