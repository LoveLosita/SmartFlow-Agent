import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import type { LoginPayload, RegisterPayload, TokenPair } from '@/types/api'
import { login as loginApi, logout as logoutApi, register as registerApi } from '@/api/auth'

const ACCESS_TOKEN_KEY = 'smartflow_access_token'
const REFRESH_TOKEN_KEY = 'smartflow_refresh_token'
const LAST_USERNAME_KEY = 'smartflow_last_username'

export const useAuthStore = defineStore('auth', () => {
  const accessToken = ref(localStorage.getItem(ACCESS_TOKEN_KEY) ?? '')
  const refreshToken = ref(localStorage.getItem(REFRESH_TOKEN_KEY) ?? '')
  const lastUsername = ref(localStorage.getItem(LAST_USERNAME_KEY) ?? '')

  const isAuthenticated = computed(() => accessToken.value.trim().length > 0)

  // applyTokenPair 只负责把后端返回的新 token 对落到内存和本地存储。
  // 职责边界：
  // 1. 负责登录成功、刷新成功后的统一持久化。
  // 2. 不负责调用接口，避免把“网络失败”和“本地状态写入”耦合在一起。
  // 3. 返回值为空；调用方若需要错误处理，应在接口层完成。
  function applyTokenPair(tokens: TokenPair) {
    accessToken.value = tokens.access_token
    refreshToken.value = tokens.refresh_token
    localStorage.setItem(ACCESS_TOKEN_KEY, tokens.access_token)
    localStorage.setItem(REFRESH_TOKEN_KEY, tokens.refresh_token)
  }

  function persistLastUsername(username: string) {
    lastUsername.value = username
    localStorage.setItem(LAST_USERNAME_KEY, username)
  }

  // clearSession 只清本地登录态，不调用后端接口。
  // 职责边界：
  // 1. 负责在 refresh 失败、401 兜底、主动退出后统一清理本地状态。
  // 2. 不负责页面跳转；跳转由调用方按场景决定，避免 store 硬绑定 UI。
  // 3. lastUsername 保留，用于下次登录时回填用户名，减少重复输入。
  function clearSession() {
    accessToken.value = ''
    refreshToken.value = ''
    localStorage.removeItem(ACCESS_TOKEN_KEY)
    localStorage.removeItem(REFRESH_TOKEN_KEY)
  }

  async function login(payload: LoginPayload) {
    const tokens = await loginApi(payload)
    applyTokenPair(tokens)
    persistLastUsername(payload.username)
    return tokens
  }

  async function register(payload: RegisterPayload) {
    const result = await registerApi(payload)
    persistLastUsername(payload.username)
    return result
  }

  // logout 负责“尽力通知后端 + 一定清理本地状态”。
  // 职责边界：
  // 1. 先请求后端注销当前 access token，让对应 jti 进入黑名单。
  // 2. 不保证后端一定成功；即使接口失败，也必须清理本地状态，避免前端假在线。
  // 3. 返回值语义：接口成功时返回后端响应；失败时向上抛错，但本地状态已被清空。
  async function logout() {
    try {
      const result = await logoutApi()
      clearSession()
      return result
    } catch (error) {
      clearSession()
      throw error
    }
  }

  return {
    accessToken,
    refreshToken,
    lastUsername,
    isAuthenticated,
    applyTokenPair,
    clearSession,
    login,
    register,
    logout,
  }
})
