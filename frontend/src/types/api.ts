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

export interface LoginPayload {
  username: string
  password: string
}

export interface RegisterPayload {
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
