export interface ApiResponse<T> {
  status: string
  info: string
  data: T
}

export interface PlainResponse {
  status: string
  info: string
}

export interface TokenPair {
  access_token: string
  refresh_token: string
}

export interface GeeTestValidateResult {
  geetest_challenge: string
  geetest_validate: string
  geetest_seccode: string
}

export interface GeeTestRegisterData {
  success: number
  gt: string
  challenge: string
  new_captcha: boolean
}

export interface LoginPayload extends GeeTestValidateResult {
  username: string
  password: string
}

export interface RegisterPayload extends GeeTestValidateResult {
  username: string
  phone_number: string
  password: string
}

export interface RegisterResult {
  id: number
}

export interface RefreshTokenPayload {
  old_refresh_token: string
}
