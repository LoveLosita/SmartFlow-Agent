import http from '@/api/http'
import type { ApiResponse } from '@/types/api'
import type { PlacedItem, SchedulePreviewData } from '@/types/dashboard'
import { extractErrorMessage } from '@/utils/http'

/**
 * 获取排程预览数据
 */
export async function getSchedulePreview(conversationId: string): Promise<SchedulePreviewData> {
  try {
    const response = await http.get<ApiResponse<SchedulePreviewData>>('/agent/schedule-preview', {
      params: { conversation_id: conversationId },
    })
    return response.data.data
  } catch (error) {
    throw new Error(extractErrorMessage(error, '获取方案预览失败'))
  }
}

/**
 * 暂存排程状态到 Redis
 */
export async function saveScheduleState(conversationId: string, items: PlacedItem[]): Promise<void> {
  try {
    await http.post<ApiResponse<void>>('/agent/schedule-state', {
      conversation_id: conversationId,
      items,
    })
  } catch (error) {
    throw new Error(extractErrorMessage(error, '暂存方案失败'))
  }
}

/**
 * 将排程结果正式应用到数据库
 */
export async function applyBatchIntoSchedule(
  taskClassId: number,
  items: PlacedItem[],
  idempotencyKey: string
): Promise<void> {
  try {
    await http.put<ApiResponse<void>>('/task-class/apply-batch-into-schedule', {
      task_class_id: taskClassId,
      items,
    }, {
      headers: {
        'Idempotency-Key': idempotencyKey
      }
    })
  } catch (error) {
    throw new Error(extractErrorMessage(error, '保存方案失败'))
  }
}
