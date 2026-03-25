import http from '@/api/http'
import type { ApiResponse, PlainResponse } from '@/types/api'
import type {
  ApplyBatchIntoScheduleItem,
  ScheduleDeletePayloadItem,
  ScheduleWeekData,
  TaskClassCreatePayload,
  TaskClassDetail,
  TaskClassListItem,
} from '@/types/schedule'
import { extractErrorMessage } from '@/utils/http'
import { createIdempotencyKey } from '@/utils/idempotency'

export async function getWeekSchedule(week?: number) {
  try {
    const response = await http.get<ApiResponse<ScheduleWeekData[]>>('/schedule/week', {
      params: typeof week === 'number' ? { week } : undefined,
    })
    return response.data.data ?? []
  } catch (error) {
    throw new Error(extractErrorMessage(error, '周日程加载失败，请稍后重试'))
  }
}

export async function getTaskClassList() {
  try {
    const response = await http.get<ApiResponse<{ task_classes: TaskClassListItem[] }>>('/task-class/list')
    return response.data.data?.task_classes ?? []
  } catch (error) {
    throw new Error(extractErrorMessage(error, '任务类列表加载失败，请稍后重试'))
  }
}

export async function getTaskClassDetail(taskClassId: number) {
  try {
    const response = await http.get<ApiResponse<TaskClassDetail>>('/task-class/get', {
      params: {
        task_class_id: taskClassId,
      },
    })
    return response.data.data
  } catch (error) {
    throw new Error(extractErrorMessage(error, '任务类详情加载失败，请稍后重试'))
  }
}

export async function createTaskClass(payload: TaskClassCreatePayload, idempotencyKey = createIdempotencyKey('task-class-add')) {
  try {
    const response = await http.post<PlainResponse>('/task-class/add', payload, {
      headers: {
        'X-Idempotency-Key': idempotencyKey,
      },
    })
    return response.data
  } catch (error) {
    throw new Error(extractErrorMessage(error, '创建任务类失败，请稍后重试'))
  }
}

export async function smartPlanning(taskClassId: number) {
  try {
    const response = await http.get<ApiResponse<ScheduleWeekData[]>>('/schedule/smart-planning', {
      params: {
        task_class_id: taskClassId,
      },
    })
    return response.data.data ?? []
  } catch (error) {
    throw new Error(extractErrorMessage(error, '智能粗排失败，请稍后重试'))
  }
}

export async function smartPlanningMulti(taskClassIds: number[]) {
  try {
    const response = await http.post<ApiResponse<ScheduleWeekData[]>>('/schedule/smart-planning-multi', {
      task_class_ids: taskClassIds,
    })
    return response.data.data ?? []
  } catch (error) {
    throw new Error(extractErrorMessage(error, '批量智能粗排失败，请稍后重试'))
  }
}

export async function applyBatchIntoSchedule(taskClassId: number, items: ApplyBatchIntoScheduleItem[], idempotencyKey = createIdempotencyKey('schedule-apply')) {
  try {
    const response = await http.put<PlainResponse>(
      '/task-class/apply-batch-into-schedule',
      {
        task_class_id: taskClassId,
        items,
      },
      {
        headers: {
          'X-Idempotency-Key': idempotencyKey,
        },
      },
    )
    return response.data
  } catch (error) {
    throw new Error(extractErrorMessage(error, '正式应用日程失败，请稍后重试'))
  }
}

export async function deleteScheduleEntries(items: ScheduleDeletePayloadItem[], idempotencyKey = createIdempotencyKey('schedule-delete')) {
  try {
    const response = await http.delete<PlainResponse>('/schedule/delete', {
      data: items,
      headers: {
        'X-Idempotency-Key': idempotencyKey,
      },
    })
    return response.data
  } catch (error) {
    throw new Error(extractErrorMessage(error, '解除安排失败，请稍后重试'))
  }
}

export async function deleteTaskClassItem(taskItemId: number, idempotencyKey = createIdempotencyKey('task-class-item-delete')) {
  try {
    const response = await http.delete<PlainResponse>('/task-class/delete-item', {
      params: {
        task_item_id: taskItemId,
      },
      headers: {
        'X-Idempotency-Key': idempotencyKey,
      },
    })
    return response.data
  } catch (error) {
    throw new Error(extractErrorMessage(error, '删除任务块失败，请稍后重试'))
  }
}
