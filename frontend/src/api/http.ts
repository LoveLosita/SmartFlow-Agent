import axios, { AxiosError, type InternalAxiosRequestConfig } from 'axios'

import router from '@/router'
import { useAuthStore } from '@/stores/auth'
import type { ApiResponse, TokenPair } from '@/types/api'

declare module 'axios' {
  interface AxiosRequestConfig {
    skipAuth?: boolean
    skipRefresh?: boolean
    _retry?: boolean
  }

  interface InternalAxiosRequestConfig {
    skipAuth?: boolean
    skipRefresh?: boolean
    _retry?: boolean
  }
}

const http = axios.create({
  baseURL: '/api/v1',
  timeout: 12000,
})

const refreshHttp = axios.create({
  baseURL: '/api/v1',
  timeout: 12000,
})

let refreshPromise: Promise<TokenPair> | null = null

function getAuthStore() {
  return useAuthStore()
}

async function redirectToAuth() {
  if (router.currentRoute.value.name !== 'auth') {
    await router.push('/auth')
  }
}

// refreshAccessToken 负责把多个并发 401 合并成一次 refresh 请求。
// 职责边界：
// 1. 负责读取当前 refresh token，并向后端换发新的 token 对。
// 2. 不负责决定“哪些请求应该触发 refresh”；这一层由响应拦截器判断。
// 3. 失败时会清理本地会话并跳回登录页，避免页面继续带着坏 token 重试。
async function refreshAccessToken() {
  if (refreshPromise) {
    return refreshPromise
  }

  const authStore = getAuthStore()
  const currentRefreshToken = authStore.refreshToken.trim()
  if (!currentRefreshToken) {
    authStore.clearSession()
    await redirectToAuth()
    throw new Error('缺少 refresh token，请重新登录')
  }

  refreshPromise = refreshHttp
    .post<ApiResponse<TokenPair>>(
      '/user/refresh-token',
      {
        old_refresh_token: currentRefreshToken,
      },
      {
        skipAuth: true,
        skipRefresh: true,
      },
    )
    .then((response) => {
      const tokens = response.data.data
      authStore.applyTokenPair(tokens)
      return tokens
    })
    .catch(async (error) => {
      authStore.clearSession()
      await redirectToAuth()
      throw error
    })
    .finally(() => {
      refreshPromise = null
    })

  return refreshPromise
}

http.interceptors.request.use((config) => {
  const authStore = getAuthStore()

  // 1. 默认所有业务请求都自动带 access token。
  // 2. 登录/注册/刷新这类公开接口通过 skipAuth 显式跳过，避免误带过期 token。
  if (!config.skipAuth && authStore.accessToken) {
    config.headers.Authorization = `Bearer ${authStore.accessToken}`
  }

  return config
})

http.interceptors.response.use(
  (response) => response,
  async (error: AxiosError) => {
    const authStore = getAuthStore()
    const originalRequest = error.config as InternalAxiosRequestConfig | undefined

    // 1. 只对“带请求配置的 401”尝试自动续签。
    // 2. 已经重试过、显式禁用 refresh 的请求，直接走失败兜底，避免死循环。
    if (
      error.response?.status !== 401 ||
      !originalRequest ||
      originalRequest.skipRefresh ||
      originalRequest._retry
    ) {
      if (error.response?.status === 401 && !originalRequest?.skipRefresh) {
        authStore.clearSession()
        await redirectToAuth()
      }
      return Promise.reject(error)
    }

    originalRequest._retry = true

    try {
      const tokens = await refreshAccessToken()
      originalRequest.headers.Authorization = `Bearer ${tokens.access_token}`
      return http(originalRequest)
    } catch (refreshError) {
      return Promise.reject(refreshError)
    }
  },
)

export default http
