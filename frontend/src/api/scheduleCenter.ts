import axios from 'axios'
import http from '@/api/http'
import type { ApiResponse, PlainResponse } from '@/types/api'
import type {
  ApplyBatchIntoScheduleItem,
  ScheduleDeletePayloadItem,
  ScheduleWeekData,
  TaskClassCreatePayload,
  TaskClassDetail,
  TaskClassListItem,
  CourseDraftRow,
  CourseImportPayload,
  CourseImageParseResponse,
} from '@/types/schedule'
import { extractErrorMessage } from '@/utils/http'
import { createIdempotencyKey } from '@/utils/idempotency'

type WeekScheduleResponseData = ScheduleWeekData | ScheduleWeekData[] | null | undefined

function normalizeWeekScheduleData(data: WeekScheduleResponseData): ScheduleWeekData[] {
  if (Array.isArray(data)) {
    return data
  }

  if (data && typeof data === 'object') {
    return [data]
  }

  return []
}

export async function getWeekSchedule(week?: number) {
  try {
    const response = await http.get<ApiResponse<WeekScheduleResponseData>>('/schedule/week', {
      params: {
        week: typeof week === 'number' ? week : 0,
      },
    })

    return normalizeWeekScheduleData(response.data.data)
  } catch (error) {
    throw new Error(extractErrorMessage(error, '\u5468\u603b\u65e5\u7a0b\u52a0\u8f7d\u5931\u8d25\uff0c\u8bf7\u7a0d\u540e\u91cd\u8bd5'))
  }
}

export async function getTaskClassList() {
  try {
    const response = await http.get<ApiResponse<{ task_classes: TaskClassListItem[] }>>('/task-class/list')
    return response.data.data?.task_classes ?? []
  } catch (error) {
    throw new Error(extractErrorMessage(error, '\u4efb\u52a1\u7c7b\u5217\u8868\u52a0\u8f7d\u5931\u8d25\uff0c\u8bf7\u7a0d\u540e\u91cd\u8bd5'))
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
    throw new Error(extractErrorMessage(error, '\u4efb\u52a1\u7c7b\u8be6\u60c5\u52a0\u8f7d\u5931\u8d25\uff0c\u8bf7\u7a0d\u540e\u91cd\u8bd5'))
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
    throw new Error(extractErrorMessage(error, '\u521b\u5efa\u4efb\u52a1\u7c7b\u5931\u8d25\uff0c\u8bf7\u7a0d\u540e\u91cd\u8bd5'))
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
    throw new Error(extractErrorMessage(error, '\u667a\u80fd\u7c97\u6392\u5931\u8d25\uff0c\u8bf7\u7a0d\u540e\u91cd\u8bd5'))
  }
}

export async function smartPlanningMulti(taskClassIds: number[]) {
  try {
    const response = await http.post<ApiResponse<ScheduleWeekData[]>>('/schedule/smart-planning-multi', {
      task_class_ids: taskClassIds,
    })
    return response.data.data ?? []
  } catch (error) {
    throw new Error(extractErrorMessage(error, '\u6279\u91cf\u667a\u80fd\u7c97\u6392\u5931\u8d25\uff0c\u8bf7\u7a0d\u540e\u91cd\u8bd5'))
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
    throw new Error(extractErrorMessage(error, '\u6b63\u5f0f\u5e94\u7528\u65e5\u7a0b\u5931\u8d25\uff0c\u8bf7\u7a0d\u540e\u91cd\u8bd5'))
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
    throw new Error(extractErrorMessage(error, '\u89e3\u9664\u5b89\u6392\u5931\u8d25\uff0c\u8bf7\u7a0d\u540e\u91cd\u8bd5'))
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
    throw new Error(extractErrorMessage(error, '\u5220\u9664\u4efb\u52a1\u5757\u5931\u8d25\uff0c\u8bf7\u7a0d\u540e\u91cd\u8bd5'))
  }
}

export async function parseCourseImage(file: File, signal?: AbortSignal) {
  try {
    const formData = new FormData()
    formData.append('image', file)
    const response = await http.post<ApiResponse<CourseImageParseResponse>>('/course/parse-image', formData, {
      timeout: 300000,
      signal,
    })
    return response.data.data
  } catch (error) {
    if (axios.isCancel(error)) {
      throw new Error('\u8bc6\u522b\u5df2\u53d6\u6d88')
    }
    throw new Error(extractErrorMessage(error, '\u8bfe\u8868\u56fe\u7247\u8bc6\u522b\u5931\u8d25\uff0c\u8bf7\u7a0d\u540e\u91cd\u8bd5'))
  }
}

export async function importCourses(payload: CourseImportPayload, idempotencyKey = createIdempotencyKey('course-import')) {
  try {
    const response = await http.post<PlainResponse>('/course/import', payload, {
      headers: {
        'X-Idempotency-Key': idempotencyKey,
      },
    })
    return response.data
  } catch (error) {
    throw new Error(extractErrorMessage(error, '\u8bfe\u7a0b\u5bfc\u5165\u5931\u8d25\uff0c\u8bf7\u7a0d\u540e\u91cd\u8bd5'))
  }
}

export async function updateTaskClass(taskClassId: number, payload: TaskClassCreatePayload, idempotencyKey = createIdempotencyKey('task-class-update')) {
  try {
    const response = await http.put<PlainResponse>('/task-class/update', payload, {
      params: {
        task_class_id: taskClassId,
      },
      headers: {
        'X-Idempotency-Key': idempotencyKey,
      },
    })
    return response.data
  } catch (error) {
    throw new Error(extractErrorMessage(error, '更新任务类失败，请稍后重试'))
  }
}
