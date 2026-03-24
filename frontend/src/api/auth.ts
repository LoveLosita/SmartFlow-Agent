import http from '@/api/http'
import { extractErrorMessage } from '@/utils/http'
import type {
  ApiResponse,
  LoginPayload,
  PlainResponse,
  RefreshTokenPayload,
  RegisterPayload,
  RegisterResult,
  TokenPair,
} from '@/types/api'

export async function login(payload: LoginPayload) {
  try {
    const response = await http.post<ApiResponse<TokenPair>>('/user/login', payload, {
      skipAuth: true,
      skipRefresh: true,
    })
    return response.data.data
  } catch (error) {
    throw new Error(extractErrorMessage(error, '登录失败，请稍后重试'))
  }
}

export async function register(payload: RegisterPayload) {
  try {
    const response = await http.post<ApiResponse<RegisterResult>>('/user/register', payload, {
      skipAuth: true,
      skipRefresh: true,
    })
    return response.data.data
  } catch (error) {
    throw new Error(extractErrorMessage(error, '注册失败，请稍后重试'))
  }
}

export async function refreshToken(payload: RefreshTokenPayload) {
  try {
    const response = await http.post<ApiResponse<TokenPair>>('/user/refresh-token', payload, {
      skipAuth: true,
      skipRefresh: true,
    })
    return response.data.data
  } catch (error) {
    throw new Error(extractErrorMessage(error, '登录状态已失效，请重新登录'))
  }
}

export async function logout() {
  try {
    const response = await http.post<PlainResponse>(
      '/user/logout',
      {},
      {
        skipRefresh: true,
      },
    )
    return response.data
  } catch (error) {
    throw new Error(extractErrorMessage(error, '退出登录失败，请稍后重试'))
  }
}
