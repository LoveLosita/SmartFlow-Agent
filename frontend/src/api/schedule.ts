import http from '@/api/http'
import type { ApiResponse } from '@/types/api'
import type { TodaySchedule } from '@/types/dashboard'
import { extractErrorMessage } from '@/utils/http'

export async function getTodaySchedule() {
  try {
    const response = await http.get<ApiResponse<TodaySchedule[]>>('/schedule/today')
    return response.data.data ?? []
  } catch (error) {
    throw new Error(extractErrorMessage(error, '今日日程加载失败，请稍后重试'))
  }
}
